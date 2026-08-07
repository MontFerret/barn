package barn

import (
	"fmt"

	"golang.org/x/mod/modfile"
	gomodule "golang.org/x/mod/module"
)

const modulePackageFilename = "go.mod"

func parseModulePackage(filename string, data []byte, version string) (string, error) {
	file, err := modfile.Parse(filename, data, nil)
	if err != nil {
		return "", err
	}

	if file.Module == nil || file.Module.Mod.Path == "" {
		return "", fmt.Errorf("module directive is required")
	}

	packagePath := file.Module.Mod.Path
	if err := gomodule.Check(packagePath, "v"+version); err != nil {
		return "", fmt.Errorf("module %q is incompatible with version %q: %w", packagePath, version, err)
	}

	return packagePath, nil
}
