package commands

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/docgen"
	"github.com/spf13/cobra"
)

func NewInternal(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "internal", Short: "Maintenance commands (hidden).", Hidden: true}
	var format, out, readme string
	var stdout bool
	command := &cobra.Command{Use: "docs", Short: "Generate avr reference docs from the live command tree.", Args: cobra.NoArgs}
	f := command.Flags()
	f.StringVar(&format, "format", "all", "Output format: markdown, json, starlight, man, all.")
	f.StringVar(&out, "out", "", "Output file or directory.")
	f.BoolVar(&stdout, "stdout", false, "Write a single-file format (markdown or json) to stdout.")
	f.StringVar(&readme, "readme", "", "README to embed as the Starlight overview page.")
	command.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if !slices.Contains([]string{"markdown", "json", "starlight", "man", "all"}, format) {
			return &ExitError{2, "Error: Invalid value for --format: choose markdown, json, starlight, man, or all."}
		}
		if f.Changed("readme") {
			if readme == "" {
				readme = "."
			}
			if info, err := os.Stat(readme); err == nil && info.IsDir() {
				return &ExitError{2, "Error: Invalid value for --readme: file is a directory."}
			}
		}
		if stdout && format != "markdown" && format != "json" {
			return &ExitError{2, "Error: --stdout is only valid with --format markdown or --format json"}
		}
		if !stdout && !f.Changed("out") {
			return &ExitError{2, "Error: --out is required (or pass --stdout for markdown/json)"}
		}
		if f.Changed("out") {
			out = filepath.Clean(out)
		}
		// Reference defaults describe the public CLI, not this invocation's
		// environment-derived flag values.
		reference := d
		reference.Getenv = func(string) string { return "" }
		reference.LookupEnv = func(string) (string, bool) { return "", false }
		tree, err := docgen.Build(NewRoot(reference))
		if err != nil {
			return err
		}
		var overview *string
		if format == "starlight" || format == "all" {
			path := readme
			if !f.Changed("readme") {
				executable, err := os.Executable()
				if err != nil {
					return err
				}
				path = filepath.Join(filepath.Dir(executable), "README.md")
			}
			if _, err := os.Stat(path); err == nil {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if !utf8.Valid(data) {
					return fmt.Errorf("README is not valid UTF-8")
				}
				value := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
				overview = &value
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		if format == "markdown" || format == "json" {
			var content string
			if format == "markdown" {
				content = docgen.Markdown(tree)
			} else {
				content, err = docgen.JSON(tree)
				if err != nil {
					return err
				}
			}
			if stdout {
				_, err = fmt.Fprint(d.Out, content)
				return err
			}
			filename := "REFERENCE.md"
			if format == "json" {
				filename = "reference.json"
			}
			target := out
			if target == "" {
				target = "."
			}
			if info, err := os.Stat(target); err == nil && info.IsDir() {
				target = filepath.Join(target, filename)
			}
			if err = writeDocFile(target, content); err != nil {
				return err
			}
			_, err = fmt.Fprintln(d.Out, "wrote "+filepath.Clean(target))
			return err
		}
		if out == "" {
			out = "."
		}
		if format == "starlight" {
			files := docgen.Starlight(tree, overview)
			if err = writeDocDir(cmd.Context(), out, files); err != nil {
				return err
			}
			_, err = fmt.Fprintf(d.Out, "wrote %d files to %s\n", len(files), filepath.Clean(out))
			return err
		}
		if format == "man" {
			target := filepath.Join(out, "man1")
			files := docgen.Man(tree)
			if err = writeDocDir(cmd.Context(), target, files); err != nil {
				return err
			}
			_, err = fmt.Fprintf(d.Out, "wrote %d files to %s\n", len(files), target)
			return err
		}
		if err = writeDocFile(filepath.Join(out, "docs", "REFERENCE.md"), docgen.Markdown(tree)); err != nil {
			return err
		}
		raw, err := docgen.JSON(tree)
		if err != nil {
			return err
		}
		if err = writeDocFile(filepath.Join(out, "docs", "reference.json"), raw); err != nil {
			return err
		}
		if err = writeDocDir(cmd.Context(), filepath.Join(out, "docs", "starlight"), docgen.Starlight(tree, overview)); err != nil {
			return err
		}
		if err = writeDocDir(cmd.Context(), filepath.Join(out, "man", "man1"), docgen.Man(tree)); err != nil {
			return err
		}
		_, err = fmt.Fprintln(d.Out, "wrote markdown, json, starlight, man under "+filepath.Clean(out))
		return err
	})
	group.AddCommand(command)
	return group
}
func writeDocFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
func writeDocDir(ctx context.Context, directory string, files map[string]string) error {
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	// Only generated-page extensions are pruned. WalkDir does not follow
	// directory symlinks outside the requested output tree.
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".md" && ext != ".1" {
			return nil
		}
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if _, exists := files[filepath.ToSlash(relative)]; !exists {
			return os.Remove(path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = writeDocFile(filepath.Join(directory, filepath.FromSlash(name)), files[name]); err != nil {
			return err
		}
	}
	return nil
}
