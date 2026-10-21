# GTS additions (Protarium)

Added on top of upstream pokemon-gen6 (commit fc45509):

- `database/gts.go` - Postgres storage (`gts.records`), reservation of listings, atomic trade completion.
- `database/gts_test.go` - database tests (`PN_POKEGEN6_TEST_POSTGRES_URI=... go test ./database`).
- `nex/gts/gts.go` - the eight Global Trade Station methods of the Pokemon Gen 6 DataStore protocol.
- `nex/register_secure_server_nex_protocols.go`, `init.go` - register them / create the schema.

Written from the protocol definitions in nex-protocols-go. A trade has been completed on real consoles (X/Y), and OR/AS connect to the same server. Known gaps:
- `SearchPokemon` ignores the `Conditions` list (the index layout they refer to is not decoded), so every listing matches.
- The signature the validator gives is stored but not verified on upload.
- The meaning of `DeleteFlag`, `Period` and `NeedData` is not interpreted.
