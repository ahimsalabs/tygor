package check

import (
	"fmt"
	"os"

	"tygor.dev/internal/discover"
	"tygor.dev/internal/runner"
)

type Cmd struct {
	Export  string `help:"Export function name (required if multiple exports exist)." short:"e"`
	Package string `help:"Package to scan (default: current directory)." short:"p" default:"."`
}

func (c *Cmd) Run() error {
	// Discover exports in the package
	result, err := discover.Find(c.Package)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}

	// Select the export to use
	export, err := discover.SelectExport(result.Exports, c.Export)
	if err != nil {
		return err
	}

	// Print export info
	fmt.Printf("✓ Found export: %s() %s\n", export.Name, export.Type)

	// Print config function if present
	if result.ConfigFunc != nil {
		fmt.Printf("✓ Found config: %s(*tygorgen.Generator) *tygorgen.Generator\n", result.ConfigFunc.Name)
	}

	// Build runner options for check mode
	opts := runner.Options{
		Export:     *export,
		CheckMode:  true,
		PkgDir:     result.Dir,
		PkgPath:    result.PackagePath,
		ModulePath: result.ModulePath,
		ModuleDir:  result.ModuleDir,
	}

	// Add config function if present and applicable
	if result.ConfigFunc != nil && export.Type == discover.ExportTypeApp {
		opts.ConfigFunc = result.ConfigFunc.Name
	}

	// Run the check
	output, err := runner.Exec(opts)
	if err != nil {
		if len(output) > 0 {
			fmt.Fprint(os.Stderr, string(output))
		}
		return err
	}

	// Parse the output: "services endpoints types\n" on stdout, warnings on stderr
	var services, endpoints, types int
	if _, err := fmt.Sscanf(string(output), "%d %d %d", &services, &endpoints, &types); err != nil {
		return fmt.Errorf("parse check output: %w\nraw output: %s", err, output)
	}

	// Print stats
	fmt.Printf("✓ %d services, %d endpoints, %d types\n", services, endpoints, types)
	fmt.Println("✓ All types resolvable")

	return nil
}
