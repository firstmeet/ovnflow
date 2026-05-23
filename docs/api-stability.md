# API stability

`ovnflow` v1.0 treats the following public surfaces as stable:

- `Connect`, `Config`, `ConfigFromEnv`, `Client`, `OVN`, `NBClient`, `SBClient`,
  and `OVSClient`.
- Typed OVN Northbound builders under `client.OVN().NB()`.
- Typed OVN Southbound list/get/watch helpers under `client.OVN().SB()`.
- Typed and generic Open_vSwitch helpers under `client.LocalOVS()`.
- Runtime `TableRef` and `TableBuilder` methods for schema-aware CRUD,
  map/set mutation, list/get, and watch.
- Error kinds and helpers: `Error`, `ErrorKind`, `IsKind`, and `KindOf`.

Compatibility rules:

- Existing stable methods will not be removed or change semantics within the
  v1 major line.
- New OVN/OVS schema columns may appear as new optional helpers.
- Optional columns continue to degrade at runtime when unsupported by the
  connected schema.
- Required table or column mismatches return `ErrorInvalidSchema`.
- Production database operations continue to use `libovsdb`.

Pre-v1 naming artifacts such as file names containing `v02` are internal
implementation details and are not part of the Go API.
