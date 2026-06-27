// Package probe performs live gNMI onboarding detection with an embedded gNMI
// client (no external binary, no shell-out). Junos gNMI Get is config-only, so we
// Subscribe ONCE to the chassis component + /system state and parse the signature
// from the typed responses.
package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"xenon/internal/model"
)

// Creds are the gNMI credentials used to probe a device.
type Creds struct {
	Username string
	Password string
}

// Result is what a live probe yields: the engine signature plus the device's
// reported hostname and the address we reached it on.
type Result struct {
	Sig      model.Signature
	Hostname string
	Addr     string
}

// Probe dials addr over gNMI (TLS, skip-verify) and runs a ONCE subscription of
// the chassis + system state, extracting a signature.
func Probe(addr string, creds Creds) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	if err != nil {
		return Result{Addr: addr}, fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	ctx = metadata.AppendToOutgoingContext(ctx, "username", creds.Username, "password", creds.Password)
	stream, err := gpb.NewGNMIClient(conn).Subscribe(ctx)
	if err != nil {
		return Result{Addr: addr}, fmt.Errorf("subscribe %s: %w", addr, err)
	}
	req := &gpb.SubscribeRequest{Request: &gpb.SubscribeRequest_Subscribe{Subscribe: &gpb.SubscriptionList{
		Mode:     gpb.SubscriptionList_ONCE,
		Encoding: gpb.Encoding_JSON,
		Subscription: []*gpb.Subscription{
			{Path: pathOf("components", elem("component", "name", "Chassis"), "state")},
			{Path: pathOf("system", "state")},
		},
	}}}
	if err := stream.Send(req); err != nil {
		return Result{Addr: addr}, fmt.Errorf("send %s: %w", addr, err)
	}

	fields := map[string]string{}
	var recvErr error
	for {
		resp, err := stream.Recv()
		if err != nil {
			if err != io.EOF {
				recvErr = err
			}
			break
		}
		if resp.GetSyncResponse() {
			break
		}
		notif := resp.GetUpdate()
		if notif == nil {
			continue
		}
		for _, upd := range notif.GetUpdate() {
			collect(lastElem(upd.GetPath()), upd.GetVal(), fields)
		}
	}

	r := Result{
		Addr: addr,
		Sig: model.Signature{
			Vendor:  fields["mfg-name"],
			Model:   fields["description"],
			Version: fields["software-version"],
		},
		Hostname: fields["hostname"],
	}
	if strings.Contains(strings.ToLower(r.Sig.Vendor), "juniper") {
		r.Sig.OS = "Junos"
	}
	if r.Hostname == "" {
		r.Hostname = addr
	}
	if r.Sig.Vendor == "" && r.Sig.Model == "" {
		if recvErr != nil {
			return r, fmt.Errorf("gNMI probe of %s failed: %w", addr, recvErr)
		}
		return r, fmt.Errorf("no chassis identity returned from %s", addr)
	}
	return r, nil
}

type pathElem struct {
	name string
	key  map[string]string
}

func elem(name, k, v string) pathElem { return pathElem{name: name, key: map[string]string{k: v}} }

// pathOf builds a gNMI path from string names and elem() entries.
func pathOf(parts ...any) *gpb.Path {
	p := &gpb.Path{}
	for _, part := range parts {
		switch x := part.(type) {
		case string:
			p.Elem = append(p.Elem, &gpb.PathElem{Name: x})
		case pathElem:
			p.Elem = append(p.Elem, &gpb.PathElem{Name: x.name, Key: x.key})
		}
	}
	return p
}

func lastElem(p *gpb.Path) string {
	if e := p.GetElem(); len(e) > 0 {
		return e[len(e)-1].GetName()
	}
	return ""
}

// collect flattens a gNMI value into name→string. A value may be a scalar (use
// the leaf name) or a JSON object subtree (use the object's own keys).
func collect(leaf string, tv *gpb.TypedValue, out map[string]string) {
	if tv == nil {
		return
	}
	switch v := tv.GetValue().(type) {
	case *gpb.TypedValue_JsonVal:
		walkJSON(leaf, v.JsonVal, out)
	case *gpb.TypedValue_JsonIetfVal:
		walkJSON(leaf, v.JsonIetfVal, out)
	case *gpb.TypedValue_StringVal:
		out[leaf] = v.StringVal
	case *gpb.TypedValue_AsciiVal:
		out[leaf] = v.AsciiVal
	}
}

func walkJSON(key string, b []byte, out map[string]string) {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return
	}
	walk(key, v, out)
}

func walk(key string, v any, out map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			walk(k, vv, out)
		}
	case string:
		out[key] = x
	case float64:
		out[key] = strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		out[key] = strconv.FormatBool(x)
	}
}
