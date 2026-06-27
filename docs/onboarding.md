# xenon — detection & onboarding flow

**Status:** draft · 2026-06-25
**Spans:** M1 (device record + lifecycle), M2 (gNMI detect + detection-rule/profile content), M5 (content
engine resolves bundled+overlay rules). Realizes the **instant-value** goal (`goals.md`).

**Goal:** point the platform at a device with minimal input → it identifies what the device is → assigns a
sensible profile → starts delivering dashboards/alerts in seconds. No SNMP-style network scan; a one-time gNMI
`Get`, not a loop.

---

## 1. Minimal input

To onboard one device you need: **`host` + a credential**. If a **default credential set** is configured
(one shared read-only service account is the common case), onboarding is effectively *just the host* — the
LibreNMS "point at it" feel. Bulk onboarding = a **provided list** (CSV / SoT), each entry run through the same
flow — still a list, never a subnet scan.

---

## 2. The detection signature (what gNMI gives us)

M2 opens a gNMI session and gathers:

| Source | Yields |
|--------|--------|
| `Capabilities` | `supported_models` ({name, org, version}) + encodings + gNMI version → vendor hints (model orgs / native namespaces) + a **capability profile** |
| `Get /openconfig-system:system/state` | `software-version` (OS + version), `hostname`, `domain-name` |
| `Get /openconfig-platform:components/component[*]/state` (chassis) | `mfg-name` (**vendor**), `part-no` / `description` (**model**), `serial-no`, `hardware-version`, `software-version` |

**Normalized signature** = `{ vendor, model, os, version, supported_models[] }`. The chassis component is usually
the richest source of vendor+model.

---

## 3. Detection rules (a content kind)

Detection rules are **content** (M2-owned schema; bundled + overlay via M5 — see `extensibility.md`). A rule maps
a signature pattern → a detected **platform** + a default **profile**:

```yaml
- id: juniper.mx304
  priority: 100
  match:                       # conditions AND'd; small condition vocabulary only
    chassis_mfg_name: "Juniper"        # contains
    chassis_model: "MX304"             # contains / regex
    # software_version: "^2[2-9]\\."   # optional regex
    # supports_models: [openconfig-network-instance]   # capability set ⊇
  platform: { vendor: juniper, family: mx, model: mx304 }
  profile:  juniper.mx304              # default profile to assign
```

- **Condition vocabulary is deliberately small** — `equals` / `contains` / `regex` on signature fields, plus
  `supports_models ⊇ {…}`. Not a DSL (minimal mental model).
- **Matching:** evaluate all effective rules against the signature; **highest `priority` matching rule wins**
  (ties → more conditions, then id). Bundled rules sit in priority bands; an **operator overlay rule can use a
  higher priority to override** (e.g. force a classification for an odd device).
- **`platform` ≠ `profile`:** platform is recorded as device metadata (display/grouping); profile is the
  collection config. A rule may map several models to one profile.

---

## 4. The fallback ladder (graceful degradation)

Most-specific to least; **a device that speaks OpenConfig always collects *something*:**

1. **Model rule** (`juniper.mx304`) → platform profile. *(active)*
2. **Vendor-generic** (`juniper.*` → `juniper.base`). *(active)*
3. **OpenConfig-generic** (supports `openconfig-interfaces`+`-system` → `openconfig.base`). *(active, but* `unclassified` *— flagged: "add a rule / confirm")*
4. **No OpenConfig at all** → `unclassified` / unsupported; surfaced (likely not a gNMI/OC target).

So "unclassified" still delivers the universal base (interfaces/system/optics/env) — it just lacks a
platform-specific profile. **Mis/under-classification is visible, never silent** (M1 lifecycle state).

---

## 5. End-to-end flow

| # | Step | Module |
|---|------|--------|
| 1 | Add device (`host` + cred) → state `pending` | M4/API or M1 (SoT sync) |
| 2 | Open gNMI session; `Capabilities` + `Get /system`,`/components` → signature | M2 |
| 3 | Resolve effective detection rules; match → platform + default profile | M2 + M5 content engine |
| 4 | Record platform + assignment + provenance (matched rule, detected-at); state → `active` (or `unclassified`/`unreachable`) | M1 |
| 5 | `inventory.changed` → compile gNMIc target (addr, cred, subs from profile + opt-ins + label-tags) → push via I3 | M2 |
| 6 | gNMIc subscribes → metrics in Prometheus; **bundled dashboards/alerts light up** → instant value | gNMIc/C3, M3, M4 |
| 7 | Operator customizes (toggle QoS, tags, reassign) → overlay → `inventory.changed` → recompile | M4 → M1 → M2 |

---

## 6. Edge / failure paths

- **Unreachable / auth fail** → state `unreachable`, surfaced; backoff + manual/scheduled retry. No profile.
- **Connects but sparse `/system`+`/components`** → classify on whatever signature exists; likely lands on a
  generic rule → `unclassified`.
- **Ambiguous (multiple equal-priority matches)** → pick by specificity then id; log the ambiguity for the
  operator (and as a signal that bundled rules need disambiguating).
- **No OpenConfig support** → `unclassified`/unsupported, surfaced.

---

## 7. Identity vs profile choice (two different axes — detection is authoritative)

**Detection is authoritative for *what the device is*.** The chassis reports its own model/vendor/version better
than any human or SoT field, so **platform identity is not routinely overridden.** A wrong detection is a
**detection-rule bug to fix in content** (fix once → everyone benefits), not a per-device manual patch.

What is **not** an override (and stays operator-controlled): **profile choice + tuning** — on a *correctly
identified* device, the operator may pick a lighter/heavier/custom profile or toggle opt-ins (e.g. QoS). That's
a collection *preference*, a separate axis from identity.

The three axes:
- **Identity (platform/model)** → owned by **detection** (authoritative).
- **Profile (what to collect)** → default from the detected platform; **operator may select/tune** (overlay).
- **Existence + grouping tags** → manual or **SoT** (P2). **SoT does NOT set model identity.**

Manual touches *identity* only at the edges, never routinely:
- **Gap-fill** when detection can't classify (`unclassified`) — the real fix is contributing a **detection
  rule** so it (and everyone) auto-classifies next time.
- A rare **pre-staging seed / mis-report escape hatch** — a provisional platform that **detection corrects on
  connect**.

---

## 8. Re-detection policy (not a loop)

- Detection runs **at onboarding**; re-runs only **on-demand** (operator "re-detect") or **on detected
  software-version change** (an upgrade may expose new models). **Never a continuous loop.**
- Re-detection **auto-applies the detected platform** (it's authoritative). **Operator profile tuning/selection
  is preserved**; the default profile only follows the platform when the device is on pure defaults. (Detection
  is trusted, so a genuine hardware/OS change re-profiling a default device is *correct*, not a surprise.)

---

## 9. Rules

- **O-R1** — detection is a **one-time / on-demand** gNMI `Get`, never a continuous discovery loop.
- **O-R2** — a device speaking OpenConfig **always gets at least the generic base profile** (graceful degradation).
- **O-R3** — classification outcome is **always visible** via M1 lifecycle state (`active`/`unclassified`/`unreachable`).
- **O-R4** — detection is **authoritative for platform identity** (not routinely overridden); a wrong detection
  is fixed in **detection-rule content**. Operator owns **profile selection/tuning** (a separate axis); SoT feeds
  **existence + tags, not identity**.
- **O-R5** — re-detection **auto-applies platform identity**; operator **profile tuning is preserved** (the
  default profile tracks the platform only when not operator-selected/tuned).

---

## 10. Deferred

- Exact **signature-field extraction** per vendor (which `/components` leaves carry model reliably) — verify empirically.
- The **default-credential** UX and credential selection on bulk onboarding.
- **Confidence scoring** for matches (beyond priority) — only if priority proves insufficient.
- Heuristic enrichment (e.g. inferring extras from observed BGP/EVPN presence) — likely unnecessary; revisit if asked.
