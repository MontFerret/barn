# Ferret Barn

Barn is the Git-backed Ferret Registry. Human-reviewed module source records
live under `modules/`; artifact-specific indexes under `catalog/` are
deterministic generated state.
Publishing a module or a version means merging a pull request that passes the
registry validation workflow. Barn does not discover releases by polling
source repositories.

## Source layout

```text
modules/
  <owner>/
    <module>/
      manifest.json
      versions/
        v<semver>.json
catalog/
  modules/
    index.json
  plugins/
    index.json
```

The canonical identity is `<owner>/<module>`. Registry manifests contain only
that identity and an anonymous HTTPS Git source. Each version record names a
Git tag and pins the exact commit to which the tag must resolve. Package
metadata comes from `ferret-module.yaml` at the pinned commit and optional
monorepo source path.

Barn consumes the Registry v1 and Module Manifest v1 contracts from
`github.com/MontFerret/specs`; it does not redefine those models.

## Development

```sh
make check               # Formatting, vet, tests, validation, catalog check.
make validate            # Validate layout and pinned source releases.
make generate            # Regenerate artifact-specific catalog indexes.
make verify              # Fail if either checked-in index is stale.
make check-immutable BASE=<git-object>
```

Remote validation uses provider-independent Git operations. It permits only
anonymous public HTTPS repositories, disables credentials and redirects, and
never checks out or executes module code.
