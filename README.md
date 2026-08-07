# Ferret Barn

Barn is the Git-backed Ferret Registry. Human-reviewed registration source
records live under `registry/`; Barn compiles them into deterministic,
artifact-specific public indexes under `catalog/`.
Publishing a module or a version means merging a pull request that passes the
registry validation workflow. Barn does not discover releases by polling
source repositories.

## Source layout

```text
registry/
  modules/
    <owner>/
      <module>/
        manifest.json
        versions/
          v<semver>.json
  plugins/
catalog/
  modules/
    index.json
  plugins/
    index.json
```

`registry/` is Barn's reviewed source tree, not a separate registry product.
Module registrations live under `registry/modules/`. `registry/plugins/` is
reserved until plugin registration contracts are defined. The `catalog/`
tree is generated and should not be edited by hand.

The canonical identity is `<owner>/<module>`. Registry manifests contain only
that identity and an anonymous HTTPS Git source. Each version record names a
Git tag and pins the exact commit to which the tag must resolve. Package
metadata comes from `ferret.yaml` at the pinned commit and optional
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

The Barn CLI's `--root` option refers to the Barn repository root containing
both `registry/` and `catalog/`, not to the `registry/` source directory.

Remote validation uses provider-independent Git operations. It permits only
anonymous public HTTPS repositories, disables credentials and redirects, and
never checks out or executes module code.
