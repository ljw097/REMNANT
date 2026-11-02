// Command remnant-release manages release signing.
//
//	remnant-release keygen -out FILE          write a new private key (base64 seed), print the public key
//	remnant-release pubkey -key FILE          print the public key of a private key
//	remnant-release sign   -key FILE SUMS     write SUMS.sig (the key may also come from $REMNANT_RELEASE_SIGNING_KEY)
//	remnant-release verify [-pub KEY] SUMS    verify SUMS.sig against KEY or the keys compiled into remnantd
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/ljw097/remnant/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: remnant-release keygen|pubkey|sign|verify [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "pubkey":
		err = pubkey(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "remnant-release:", err)
		os.Exit(1)
	}
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "", "file to write the private key to (must not exist)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func loadKey(file string) (ed25519.PrivateKey, error) {
	if file == "" {
		if v := os.Getenv("REMNANT_RELEASE_SIGNING_KEY"); v != "" {
			return update.ParsePrivateKey(v)
		}
		return nil, errors.New("-key or $REMNANT_RELEASE_SIGNING_KEY is required")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return update.ParsePrivateKey(string(b))
}

func pubkey(args []string) error {
	fs := flag.NewFlagSet("pubkey", flag.ContinueOnError)
	file := fs.String("key", "", "private key file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	k, err := loadKey(*file)
	if err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey)))
	return nil
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	file := fs.String("key", "", "private key file (default $REMNANT_RELEASE_SIGNING_KEY)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: sign [-key FILE] SHA256SUMS")
	}
	k, err := loadKey(*file)
	if err != nil {
		return err
	}
	sums, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	return os.WriteFile(fs.Arg(0)+".sig", []byte(update.Sign(k, sums)+"\n"), 0o644)
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	pub := fs.String("pub", "", "base64 public key (default: the keys compiled into remnantd)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: verify [-pub KEY] SHA256SUMS")
	}
	var keys []ed25519.PublicKey
	if *pub != "" {
		k, err := update.ParsePublicKey(*pub)
		if err != nil {
			return err
		}
		keys = []ed25519.PublicKey{k}
	} else {
		var err error
		if keys, err = update.TrustedKeys(); err != nil {
			return err
		}
	}
	sums, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(fs.Arg(0) + ".sig")
	if err != nil {
		return err
	}
	if err := update.Verify(keys, sums, string(sig)); err != nil {
		return err
	}
	fmt.Println("signature OK")
	return nil
}
