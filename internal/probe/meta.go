package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"time"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// Meta is per-device static config metadata captured out-of-band (not via the
// Prometheus scrape, which suppresses static leaves). Keyed names: interface_name
// and BGP neighbor-address.
type Meta struct {
	Interfaces map[string]string // interface_name -> description
	BGP        map[string]string // neighbor-address -> description
}

// Descriptions captures interface + BGP-neighbour descriptions via a gNMI ONCE
// subscription. Junos honours the leaf-subset path for ONCE (unlike streaming), so
// this is cheap and exact. Static metadata belongs in the app, joined at render.
func Descriptions(addr string, creds Creds) (Meta, error) {
	m := Meta{Interfaces: map[string]string{}, BGP: map[string]string{}}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	if err != nil {
		return m, fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	ctx = metadata.AppendToOutgoingContext(ctx, "username", creds.Username, "password", creds.Password)
	stream, err := gpb.NewGNMIClient(conn).Subscribe(ctx)
	if err != nil {
		return m, err
	}
	req := &gpb.SubscribeRequest{Request: &gpb.SubscribeRequest_Subscribe{Subscribe: &gpb.SubscriptionList{
		Mode:     gpb.SubscriptionList_ONCE,
		Encoding: gpb.Encoding_JSON,
		Subscription: []*gpb.Subscription{
			{Path: pathOf("interfaces", "interface", "state", "description")},
			{Path: pathOf("network-instances", "network-instance", "protocols", "protocol", "bgp", "neighbors", "neighbor", "state", "description")},
		},
	}}}
	if err := stream.Send(req); err != nil {
		return m, err
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return m, err
		}
		if resp.GetSyncResponse() {
			break
		}
		notif := resp.GetUpdate()
		if notif == nil {
			continue
		}
		for _, upd := range notif.GetUpdate() {
			elems := append(append([]*gpb.PathElem{}, notif.GetPrefix().GetElem()...), upd.GetPath().GetElem()...)
			val := leafString(upd.GetVal())
			if val == "" {
				continue
			}
			if n := keyOf(elems, "interface", "name"); n != "" {
				m.Interfaces[n] = val
			}
			if nb := keyOf(elems, "neighbor", "neighbor-address"); nb != "" {
				m.BGP[nb] = val
			}
		}
	}
	return m, nil
}

// keyOf returns the value of key on the named path element, or "".
func keyOf(elems []*gpb.PathElem, name, key string) string {
	for _, e := range elems {
		if e.GetName() == name {
			return e.GetKey()[key]
		}
	}
	return ""
}

// leafString extracts a string leaf value (JSON-encoded or scalar).
func leafString(tv *gpb.TypedValue) string {
	switch v := tv.GetValue().(type) {
	case *gpb.TypedValue_StringVal:
		return v.StringVal
	case *gpb.TypedValue_AsciiVal:
		return v.AsciiVal
	case *gpb.TypedValue_JsonVal:
		var s string
		if json.Unmarshal(v.JsonVal, &s) == nil {
			return s
		}
	case *gpb.TypedValue_JsonIetfVal:
		var s string
		if json.Unmarshal(v.JsonIetfVal, &s) == nil {
			return s
		}
	}
	return ""
}
