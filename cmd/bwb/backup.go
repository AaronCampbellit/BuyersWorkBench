package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"buyersworkbench/internal/backup"
)

func runBackup(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || (args[0] != "backup-key" && args[0] != "backup-encrypt" && args[0] != "backup-decrypt") {
		return false, nil
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	keyPath := flags.String("key-file", "", "restricted encryption key path")
	var output, input string
	var stdout bool
	if args[0] == "backup-encrypt" {
		flags.StringVar(&output, "output", "", "new encrypted output path")
	}
	if args[0] == "backup-decrypt" {
		flags.StringVar(&input, "input", "", "encrypted backup path")
		flags.BoolVar(&stdout, "stdout", false, "write decrypted data to a restore pipe")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *keyPath == "" {
		return true, errors.New("usage: bwb backup-key --key-file PATH | backup-encrypt --key-file PATH --output PATH < dump | backup-decrypt --key-file PATH --input PATH --stdout | pg_restore")
	}
	switch args[0] {
	case "backup-key":
		if err := backup.GenerateKey(*keyPath); err != nil {
			return true, err
		}
		return true, json.NewEncoder(out).Encode(map[string]any{"key_created": true})
	case "backup-encrypt":
		if output == "" {
			return true, errors.New("encrypted output path is required")
		}
		size, err := backup.EncryptFile(os.Stdin, *keyPath, output)
		if err != nil {
			return true, err
		}
		return true, json.NewEncoder(out).Encode(map[string]any{"encrypted": true, "bytes": size})
	case "backup-decrypt":
		if !stdout || input == "" {
			return true, errors.New("decryption requires --input PATH --stdout and a restore pipe")
		}
		if file, ok := out.(*os.File); ok {
			if info, err := file.Stat(); err != nil || info.Mode()&os.ModeCharDevice != 0 {
				return true, errors.New("refusing to print database contents to a terminal; use a restore pipe")
			}
		}
		return true, backup.DecryptFile(*keyPath, input, out)
	}
	return true, nil
}
