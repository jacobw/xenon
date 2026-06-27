# xenon — M3 Alarming: module design

**Status:** draft · 2026-06-26
**Parent:** `docs/c4-application.md` (decomposes module **M3**). Reads metrics + availability signals via **M2.d**
(P1/I4); delivers via **P3** (I7); rules / routing / windows are **content** (M5 engine).

**Engine choice: app-native** (not Prometheus-ruler + Alertmanager) — for NMS-grade *lifecycle*
(ack/clear/correlate/history) that Alertmanager doesn't do, alarms first-class in the DB, and one fewer service
to run. Accepted tradeoff: app-down = no evaluation (mitigate later via app HA or a minimal Prometheus-ruler
backstop for a few critical alarms).

> Handles: parts `M3.a–M3.d`, module rules `M3-R#`.

---

## Parts (four, one module)

```
   M3.a Rules (content) → M3.b Evaluation (loop) → M3.c Lifecycle (state/correlate/suppress) → M3.d Notification (P3)
                                                            │ emits alarm.* events
                                                            └────────────▶ also consumed by M4 (live UI/SSE)
```

---

## M3.a — Rules (a content kind)

- A rule = `{ condition, for-duration, severity, scope, routing hints, identity labels }`.
- **Condition — hybrid:** a **templated** form (metric + comparator + threshold) for the common case + UI,
  compiled to PromQL **via M2.d** (so the label schema stays encapsulated and rules are **vendor-portable**);
  plus a **raw-PromQL escape hatch** for advanced rules. Validated against the M2.a schema **at save** (loud
  failure, not silent at eval).
- **Scope** = a tag/group selector (M1 tags) — which devices the rule applies to.
- Bundled **alert packs** ship sensible defaults (interface down, high CPU, optics degraded, BGP session down,
  device unreachable, …); operator **overlay** adds/edits/silences (the extensibility model).

---

## M3.b — Evaluation

- A scheduled loop (M5 scheduler; **singleton-per-cluster**, AINV-3) evaluates the effective rules via **M2.d**
  on a tick; produces, per rule, the set of firing label-sets; applies `for` (pending → firing) itself.
- **Availability alarms are just rules over M2.d signals:** `TelemetryUp` / `CollectorUp` + the
  **single-vs-many** heuristic (many devices' telemetry down at once → **one** collector/segment alarm, not N).
- *Design note:* evaluation is app-side (M3 issues PromQL via M2.d). If eval load ever bites, the **same rule
  abstraction** can push evaluation into Prometheus's ruler and read back `ALERTS` — an optimization behind
  M3's interface, not a redesign.

---

## M3.c — Lifecycle (the NMS-grade part)

- **Alarm identity = (rule + identifying labels)** (e.g. `interface-down` + `{device, name}`). The same firing
  across cycles is the **same alarm** (dedup), not a new one each tick (M3-R2).
- **States:** `active` → `acknowledged` (operator; stops re-notify, stays visible) → `cleared` (condition
  resolved automatically, or manual). Plus `flapping` (dampened) and `suppressed`.
- **History:** every transition recorded (audit + reporting).
- **Correlation — base = same-device parent suppression:** while a device's `device-down` / `telemetry-down`
  alarm is active, that device's child alarms (its interfaces/BGP) are marked **suppressed** and **not notified**
  — one notification for a dead device, not 50. Cross-device / topology root-cause correlation needs adjacency
  data → **deferred** (ties to neighbor-corroboration).
- **Maintenance windows:** a window = `{ time range / recurrence, scope selector (tags/groups), mode }`. During
  a matching active window, matching alarms are **suppressed (not notified)** and marked *in-maintenance*.
  (Closes the earlier tally GAP.)
- Emits `alarm.raised` / `alarm.cleared` / `alarm.changed` events.

---

## M3.d — Notification (delivery ≠ detection, AINV-4)

- Subscribes to alarm events — **as does M4** (live UI/SSE). Independent subscribers via a simple interface, **no
  event bus**.
- **Routing:** map alarm (severity / tags / scope) → notifier(s) + recipients; **throttle / group** (coalesce,
  don't send 100 messages); **basic escalation** (unacked for N min → re-notify/escalate; advanced escalation
  deferred).
- **Notifiers behind P3** (SMTP / Slack / webhook / PagerDuty) — pluggable adapters.
- Routing config is content/overlay (bundled defaults + operator routes).

---

## Module rules

- **M3-R1** — conditions go through **M2.d** (typed query API), never raw Prometheus — encapsulates the label
  schema; rules stay vendor-portable + validated.
- **M3-R2** — alarm identity = (rule + identifying labels); dedup across eval cycles.
- **M3-R3** — **detection ≠ delivery** (AINV-4): lifecycle emits events; notification + UI are independent subscribers.
- **M3-R4** — **device-down suppresses that device's child alarms** (base correlation); topology correlation deferred.
- **M3-R5** — **maintenance windows** suppress notification + mark *in-maintenance* for matching scope/time.
- **M3-R6** — eval loop is **singleton-per-cluster** (AINV-3); app-down = no eval (accepted; HA/backstop later).
- **M3-R7** — rules + routing + windows are **content** (bundled + overlay), validated against M2.a at save.

---

## Representative flows

- **Threshold:** tick → M2.d query → firing `{device, if}` ≥ `for` → active alarm → `raised` → P3 notify + UI
  SSE. Condition resolves → `cleared` → recovery notify.
- **Device-down storm:** device-down active → that device's child alarms suppressed → **one** notification.
- **Maintenance:** window active for `site=dc1` → syd alarms suppressed (in-maintenance), no pages.
- **Ack:** operator acks → re-notify/escalation stops; alarm stays visible until cleared.

---

## Deferred

- Topology / cross-device root-cause correlation (needs adjacency data).
- Advanced escalation policies / on-call schedules.
- Alarm → ticketing integrations (just another P3 notifier kind).
- Eval-in-Prometheus-ruler optimization (only if app-side eval load bites).
