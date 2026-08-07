package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/MontFerret/barn/internal/barn"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "barn:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("usage: barn <validate|generate|verify|check-immutable|stamp> [options]")
	}

	command := arguments[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	root := flags.String("root", ".", "Barn repository root containing registry/; dist/ is generated there")
	base := flags.String("base", "", "base Git object for immutability validation")
	timestamp := flags.String("timestamp", "", "publication timestamp for stamp (defaults to the current UTC time)")
	check := flags.Bool("check", false, "report whether all versions are stamped without modifying files")

	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}

	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	if command == "check-immutable" {
		if *base == "" {
			return fmt.Errorf("check-immutable requires --base")
		}

		return barn.CheckImmutable(ctx, *root, *base)
	}

	if command == "stamp" {
		if *check {
			if *timestamp != "" {
				return fmt.Errorf("stamp --check does not accept --timestamp")
			}

			return barn.CheckVersionsStamped(*root)
		}

		publishedAt := time.Now()
		if *timestamp != "" {
			var err error
			publishedAt, err = time.Parse(time.RFC3339, *timestamp)
			if err != nil {
				return fmt.Errorf("parse publication timestamp: %w", err)
			}
		}

		_, err := barn.StampVersions(*root, publishedAt)

		return err
	}

	if command != "validate" && command != "generate" && command != "verify" {
		return fmt.Errorf("unknown command %q", command)
	}

	registry, err := barn.Validate(ctx, *root, barn.GitInspector{})
	if err != nil {
		return err
	}

	if command == "validate" {
		return nil
	}

	distribution, err := barn.GenerateDistribution(registry)
	if err != nil {
		return err
	}

	if command == "generate" {
		return barn.WriteDistribution(*root, distribution)
	}

	return barn.VerifyDistribution(*root, distribution)
}
