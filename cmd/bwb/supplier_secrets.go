package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"

	"buyersworkbench/internal/secretvault"
)

const supplierSecretUsage = "usage: bwb supplier-secret-key --key-file PATH | supplier-secret-set --connection UUID --key-file PATH --vault-dir PATH --value-file PATH | supplier-secret-status --connection UUID --key-file PATH --vault-dir PATH | supplier-secret-delete --connection UUID --key-file PATH --vault-dir PATH"

// Supplier secrets are a local-only administration surface, without database
// access, transport activation, supplier contact, or a plaintext output command.
func runSupplierSecrets(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "supplier-secret-key", "supplier-secret-set", "supplier-secret-status", "supplier-secret-delete":
	default:
		return false, nil
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	keyPath := flags.String("key-file", "", "restricted raw 32-byte key file")
	var connectionID, vaultDir, valueFile string
	if args[0] != "supplier-secret-key" {
		flags.StringVar(&connectionID, "connection", "", "supplier connection UUID")
		flags.StringVar(&vaultDir, "vault-dir", "", "restricted local vault directory")
	}
	if args[0] == "supplier-secret-set" {
		flags.StringVar(&valueFile, "value-file", "", "restricted local credential or session file")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *keyPath == "" {
		return true, errors.New(supplierSecretUsage)
	}
	encoder := json.NewEncoder(out)
	if args[0] == "supplier-secret-key" {
		if err := secretvault.GenerateKey(*keyPath); err != nil {
			return true, err
		}
		return true, encoder.Encode(map[string]any{"key_created": true})
	}
	if _, err := secretvault.Reference(connectionID); err != nil || vaultDir == "" || (args[0] == "supplier-secret-set" && valueFile == "") {
		return true, errors.New(supplierSecretUsage)
	}
	vault, err := secretvault.Open(vaultDir, *keyPath)
	if err != nil {
		return true, err
	}
	defer vault.Close()
	switch args[0] {
	case "supplier-secret-set":
		value, err := secretvault.ReadValueFile(valueFile)
		if err != nil {
			return true, err
		}
		defer secretvault.Clear(value)
		metadata, err := vault.Put(connectionID, value)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(map[string]any{"stored": true, "metadata": metadata})
	case "supplier-secret-status":
		metadata, err := vault.Status(connectionID)
		if errors.Is(err, secretvault.ErrNotFound) {
			reference, _ := secretvault.Reference(connectionID)
			return true, encoder.Encode(map[string]any{"configured": false, "secret_ref": reference})
		}
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(map[string]any{"configured": true, "metadata": metadata})
	case "supplier-secret-delete":
		err := vault.Delete(connectionID)
		if err != nil && !errors.Is(err, secretvault.ErrNotFound) {
			return true, err
		}
		return true, encoder.Encode(map[string]any{"deleted": true})
	}
	return true, nil
}
