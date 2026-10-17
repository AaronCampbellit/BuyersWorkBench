// bwb provides local, privileged setup; it exposes no public registration path.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"buyersworkbench/internal/store"
	"buyersworkbench/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "bwb:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: bwb migrate | bootstrap-owner --email EMAIL --name NAME --password-file PATH | status")
	}
	if handled, err := runBackup(args, out); handled {
		return err
	}
	if handled, err := runSupplierSecrets(args, out); handled {
		return err
	}
	if handled, err := runRetention(ctx, args, out); handled {
		return err
	}
	if handled, err := runWorkbench(ctx, args, out); handled {
		return err
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(out, "Usage: bwb migrate | bootstrap-owner --email EMAIL --name NAME --password-file PATH | status"+
			"\nWorkbench: bwb seed --workspace ID | create-reviewer --email EMAIL --name NAME --password-file PATH [--expires RFC3339] | disable-reviewer --user-id ID | reset-reviewer-password --user-id ID --password-file PATH"+
			"\nSupplier secrets: bwb supplier-secret-key --key-file PATH | supplier-secret-set --connection UUID --key-file PATH --vault-dir PATH --value-file PATH | supplier-secret-status --connection UUID --key-file PATH --vault-dir PATH | supplier-secret-delete --connection UUID --key-file PATH --vault-dir PATH"+
			"\nRetention: bwb retention-plan --workspace UUID --days 30 [--limit 1000] | retention-apply --workspace UUID --before RFC3339 --confirm [--limit 1000]"+
			"\nDatabase: DATABASE_URL_FILE (preferred) or DATABASE_URL. Use the privileged local role for administration."+
			"\nPassword files must be regular files readable only by their owner, with 14+ characters."+
			"\nRepeating bootstrap-owner or create-reviewer preserves existing credentials.")
		return err
	}
	var owner store.OwnerInput
	var passwordFile string
	switch args[0] {
	case "migrate", "status":
		if len(args) != 1 {
			return errors.New("this command accepts no arguments")
		}
	case "bootstrap-owner":
		flags := flag.NewFlagSet("bootstrap-owner", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.StringVar(&owner.Email, "email", "", "owner email")
		flags.StringVar(&owner.DisplayName, "name", "", "owner display name")
		flags.StringVar(&passwordFile, "password-file", "", "restricted password file")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return errors.New("usage: bwb bootstrap-owner --email EMAIL --name NAME --password-file PATH")
		}
	default:
		return errors.New("unknown command; use bwb --help")
	}
	pool, err := store.Open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	switch args[0] {
	case "migrate":
		if err := migrations.Apply(ctx, pool); err != nil {
			return err
		}
		if err := store.MigrateQueue(ctx, pool); err != nil {
			return err
		}
		return encoder.Encode(map[string]any{"schema_ready": true, "queue_schema_ready": true})
	case "status":
		status, err := store.Status(ctx, pool)
		if err != nil {
			return err
		}
		return encoder.Encode(status)
	case "bootstrap-owner":
		if passwordFile != "" {
			password, err := readPasswordFile(passwordFile)
			if err != nil {
				return err
			}
			defer func() {
				for i := range password {
					password[i] = 0
				}
			}()
			owner.Password = string(password)
		}
		result, err := store.BootstrapOwner(ctx, pool, owner)
		if err != nil {
			return err
		}
		return encoder.Encode(result)
	}
	return nil
}

func readPasswordFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open owner password file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("owner password file must be a regular file with permissions 0600 or 0400")
	}
	password, err := io.ReadAll(io.LimitReader(file, 1027))
	if err != nil || len(password) > 1026 {
		return nil, errors.New("cannot read owner password file or file exceeds the password limit")
	}
	// Remove only the single line ending commonly written by a password prompt.
	if len(password) > 0 && password[len(password)-1] == '\n' {
		password = password[:len(password)-1]
		if len(password) > 0 && password[len(password)-1] == '\r' {
			password = password[:len(password)-1]
		}
	}
	return password, nil
}
