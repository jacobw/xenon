// Command xenond is a prototype of the xenon control/read plane with a web GUI.
// IA (docs/gui-architecture.md): a monitoring plane (Overview, Devices with
// per-device tabs, Explore multi-device graphing, Alarms) keyed on operational
// status, and a config/engine plane (per-device Config tab + Admin). The M3
// alarm engine evaluates content rules against the metric store on a loop.
// Server-rendered (net/http + html/template); HTMX (CDN) for interactions.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"xenon/internal/alarms"
	"xenon/internal/content"
	"xenon/internal/inventory"
	"xenon/internal/metrics"
	"xenon/internal/model"
	"xenon/internal/persist"
	"xenon/internal/probe"
)

//go:embed templates/*.html
var tmplFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"stateClass": stateClass,
	"sevClass":   sevClass,
	"dict":       dict,
}).ParseFS(tmplFS, "templates/*.html"))

// dict builds a map from alternating key/value args, for passing several values
// into a nested template (e.g. the shared graph "panel").
func dict(kv ...any) map[string]any {
	m := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// stateClass maps an engine lifecycle state to a CSS pill class (Config tab only).
func stateClass(state string) string {
	switch {
	case strings.HasPrefix(state, "active"):
		return "ok"
	case strings.HasPrefix(state, "unclassified"):
		return "warn"
	default:
		return "bad"
	}
}

// sevClass maps an alarm severity to a CSS pill class.
func sevClass(sev string) string {
	switch sev {
	case "critical":
		return "bad"
	case "warning":
		return "warn"
	default:
		return "mut"
	}
}

type indexData struct {
	Title string
	Rows  []deviceRow
	Total int
}

type deviceData struct {
	Title      string
	Dev        inventory.Onboarded
	Op         opStatus
	Tab        string
	Metrics    metrics.DeviceMetrics
	Graphs     []graph
	Ports      []port
	Optics     []optic
	Routing    []bgpPeer
	Health     []graph
	Components []component
	Alarms     []alarms.Alarm
	IfCount    int
	AlarmCount int
	ConfigJSON string
}

type previewData struct {
	Addr string
	Res  probe.Result
	O    inventory.Onboarded
}

type adminData struct {
	Title    string
	Rules    []model.DetectionRule
	Profiles []profileRow
}

type alarmsData struct {
	Title   string
	Active  []alarms.Alarm
	Cleared []alarms.Alarm
}

func main() {
	store, err := content.LoadBundled()
	if err != nil {
		log.Fatal("load content: ", err)
	}
	dbPath := os.Getenv("XENON_DB")
	if dbPath == "" {
		dbPath = "xenon.db"
	}
	db, err := persist.Open(dbPath)
	if err != nil {
		log.Fatal("open device store: ", err)
	}
	defer db.Close()
	log.Printf("device store: SQLite at %s", dbPath)
	inv, err := inventory.NewStore(store, db)
	if err != nil {
		log.Fatal("load inventory: ", err)
	}
	mc := metrics.New(os.Getenv("XENON_PROM"))
	if mc.Enabled() {
		log.Printf("read-plane: Prometheus at %s", os.Getenv("XENON_PROM"))
	}
	alarmStore := alarms.NewStore(store.Alerts)
	go alarmStore.Run(mc, 15*time.Second)
	onboardCreds := probe.Creds{Username: os.Getenv("GNMIC_USERNAME"), Password: os.Getenv("GNMIC_PASSWORD")}

	mux := http.NewServeMux()

	// ---- monitoring plane ----

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, "overview", buildOverview(mc, inv, alarmStore))
	})

	mux.HandleFunc("GET /devices", func(w http.ResponseWriter, r *http.Request) {
		reachable := mc.Reachable()
		bySrc := mc.VectorBy(`count by (source)({job="gnmic"})`, "source")
		alc := alarmStore.CountsByDevice()
		var rows []deviceRow
		total := 0
		for _, o := range inv.List() {
			rows = append(rows, deviceRow{O: o, Op: opFromSeries(reachable, bySrc, o.Device.MgmtAddress), Alarms: alc[o.Device.Hostname]})
			total += o.EstSeries
		}
		render(w, "inventory", indexData{Title: "Devices", Rows: rows, Total: total})
	})

	mux.HandleFunc("GET /device/{id}", func(w http.ResponseWriter, r *http.Request) {
		o, ok := inv.Get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		src := o.Device.MgmtAddress
		tab := r.URL.Query().Get("tab")
		dd := deviceData{Title: o.Device.Hostname, Dev: o, Op: deviceOp(mc, src), Tab: tab}
		switch tab {
		case "ports":
			dd.Ports = buildPorts(mc, src)
		case "optics":
			dd.Optics = buildOptics(mc, src)
		case "routing":
			dd.Routing = buildRouting(mc, src)
		case "health":
			dd.Health = buildHealth(mc, src)
			dd.Components = buildComponents(mc, src)
		case "alarms":
			dd.Alarms = alarmStore.ForDevice(o.Device.Hostname)
		case "config":
			b, _ := json.MarshalIndent(o.Config, "", "  ")
			dd.ConfigJSON = string(b)
		default:
			dd.Tab = "overview"
			dd.Metrics = mc.ForDevice(src)
			dd.Graphs = buildGraphs(mc, src)
			if n, ok := mc.Scalar(fmt.Sprintf(`count(interfaces_interface_state_counters_in_octets{source=%q})`, src)); ok {
				dd.IfCount = int(n)
			}
			dd.AlarmCount = len(alarmStore.ForDevice(o.Device.Hostname))
		}
		render(w, "device", dd)
	})

	// per-device graph drill-down (HTMX)
	mux.HandleFunc("GET /device/{id}/graph", func(w http.ResponseWriter, r *http.Request) {
		o, ok := inv.Get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		gd, ok := buildGraphDetail(mc, o.Device.ID, o.Device.MgmtAddress, q.Get("m"), q.Get("iface"), q.Get("r"))
		if !ok {
			render(w, "graph-empty", nil)
			return
		}
		render(w, "graph-detail", gd)
	})

	// fleet-wide graph drill-down (HTMX)
	mux.HandleFunc("GET /graph", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		gd, ok := buildGlobalGraphDetail(mc, q.Get("m"), q.Get("r"))
		if !ok {
			render(w, "graph-empty", nil)
			return
		}
		render(w, "graph-detail", gd)
	})

	// Explore: multi-device / group-by graphing
	mux.HandleFunc("GET /explore", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		render(w, "explore", buildExplorePage(q.Get("m"), q.Get("by"), q.Get("r")))
	})
	mux.HandleFunc("GET /explore/g", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		render(w, "explore-graph", buildExploreGraph(mc, q.Get("m"), q.Get("by"), q.Get("r")))
	})

	// Alarms (M3 app-native engine)
	mux.HandleFunc("GET /alarms", func(w http.ResponseWriter, r *http.Request) {
		render(w, "alarms", alarmsData{Title: "Alarms", Active: alarmStore.Active(), Cleared: alarmStore.Cleared()})
	})
	mux.HandleFunc("POST /alarms/ack", func(w http.ResponseWriter, r *http.Request) {
		alarmStore.Ack(r.FormValue("key"))
		render(w, "alarms-table", alarmsData{Active: alarmStore.Active(), Cleared: alarmStore.Cleared()})
	})

	// ---- collection control plane (I3: gNMIc http loader pulls targets) ----

	mux.HandleFunc("GET /gnmic/targets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildGnmicTargets(inv))
	})

	// ---- config / engine plane ----

	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		render(w, "admin", adminData{Title: "Admin", Rules: store.DetectionRules, Profiles: profileRows(store)})
	})

	mux.HandleFunc("POST /onboard/detect", func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimSpace(r.FormValue("addr"))
		if addr == "" {
			render(w, "detect-error", map[string]string{"Err": "Enter a gNMI address (host:port)."})
			return
		}
		res, err := probe.Probe(addr, onboardCreds)
		if err != nil {
			render(w, "detect-error", map[string]string{"Err": err.Error()})
			return
		}
		render(w, "detect-preview", previewData{Addr: addr, Res: res, O: inv.Preview(res.Sig, res.Hostname, res.Addr)})
	})

	mux.HandleFunc("POST /onboard", func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimSpace(r.FormValue("addr"))
		res, err := probe.Probe(addr, onboardCreds)
		if err != nil {
			render(w, "detect-error", map[string]string{"Err": err.Error()})
			return
		}
		o := inv.Preview(res.Sig, res.Hostname, res.Addr)
		if err := inv.Add(o); err != nil {
			render(w, "detect-error", map[string]string{"Err": "Could not save device: " + err.Error()})
			return
		}
		w.Header().Set("HX-Redirect", "/device/"+o.Device.ID)
		w.WriteHeader(http.StatusOK)
	})

	addr := os.Getenv("XENON_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("xenond listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
