# Health monitoring strategy — what to collect & how to measure (Juniper)

The device's component tree has many entries that **can't meaningfully fail** or
have no measurable health signal — the chassis itself, the CPU-as-a-component,
generic FRUs, mezzanines, port shells, transceiver shells. Monitoring all of them
is noise. This fixes a principled *what to monitor* for Juniper, grounded in how
LibreNMS does it.

## What LibreNMS does (Junos)

Source: `resources/definitions/os_discovery/junos.yaml` (LibreNMS). It cleanly
separates **MONITORING** (sensors with a value/state + thresholds) from
**INVENTORY** (the entity-physical tree, display only), and is *selective* via
`skip_values`:

- **temperature / cpu / memory** — from `jnxOperatingTable`, but `skip_values`
  exclude `jnxOperatingMemory = 0` (non-compute entities) and descr matching
  `/(fan|sensor)/i`. So only real operating entities (RE, FPC) get temp/cpu/mem.
- **optics (dbm)** — `JUNIPER-DOM-MIB` Rx/Tx/lane power + module temp + Tx bias,
  `skip_values: 0` (only *lit* optics), grouped `transceiver`, labelled by the
  interface description (`ifDescr`).
- **state** — chassis **Yellow/Red alarm** (`off`=ok / `on`=critical); **`jnxFruState`**
  for every FRU (`jnxFruName`) with a rich map: `empty`→unknown(grey),
  `ready`/`online`→ok, `offline`→critical, `standby`/`diagnostic`→unknown; PoE
  controller (`on`/`off`/`fault`). Generic state codes: `0`=ok `1`=warning
  `2`=critical `3`=unknown.

**Takeaway:** LibreNMS monitors the **states of failable FRUs** (PSU, fan, card, RE)
and **sensors** — not the whole inventory — and `skip_values` drop the meaningless.

## xenon strategy (OpenConfig / gNMI, generic for Juniper)

**MONITOR** (health — thresholds / state maps, can raise alarms):
- **Sensors:** temperature, CPU, memory; optics dBm Rx/Tx/bias — skip when absent/0.
- **Failable component states:** `oper-status` of *failable component types only* —
  `POWER_SUPPLY`, `FAN`, `LINECARD`, `CONTROLLER_CARD`, `FABRIC` (the chassis
  subsystems whose failure has operational impact). State map: `ACTIVE`→ok,
  `DISABLED`→warn (absent / unpowered — worth a look, not a hard fault),
  anything else (`INACTIVE`/fault)→critical.
- *(future)* device self-alarms via `/system/alarms` if the platform exposes it.

**EXCLUDE from health** (can't fail, or covered elsewhere):
`CHASSIS` (always present) · `CPU`-component (covered by the CPU metric) · `PORT`
(interface oper-status) · `TRANSCEIVER` (optics dBm) · `SENSOR` (temperature) ·
`INTEGRATED_CIRCUIT` / `STORAGE` / `MEZZ` / generic `FRU`.

**INVENTORY** (display only, no alarms): every component with a serial/part — the
entity-physical equivalent. Stays comprehensive; it's not "monitoring".

This is **generic for Juniper**: the component *types* are OpenConfig-standard, and
the failable-type allowlist + state map apply across Junos platforms (EX/MX/QFX/SRX)
— no per-model tables (the win over LibreNMS's per-MIB SNMP approach).

## How to measure

- **Sensors** → the LibreNMS 4-threshold band (`low_limit` / `low_warn` / `warn` /
  `high`) → ok/warn/crit colour + alarm. (Optics dBm already; temperature threshold
  is the next sensor to band.)
- **Failable component states** → the ok/warn/crit map above → Health *Components*
  table colour, and a `component-fault` alarm for a failable component in a fault
  state.
