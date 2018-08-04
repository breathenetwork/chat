package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"github.com/breathenetwork/chat/broker"
	"golang.org/x/crypto/ssh"
	"io/ioutil"
	"os"
)

func main() {
	id := "main"
	flag.StringVar(&id, "id", id, "broker id")
	addr := ":31337"
	flag.StringVar(&addr, "addr", addr, "address to bind")
	backend := "127.0.0.1:31335"
	flag.StringVar(&backend, "backend", backend, "backend to connect to")
	key := "privkey.pem"
	flag.StringVar(&key, "key", key, "private key file")

	flag.Parse()

	if _, err := os.Stat(key); err != nil {
		if f, err := os.Create(key); err != nil {
			fmt.Println(err)
		} else {
			if key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
				fmt.Println(err)
			} else {
				if m, err := x509.MarshalECPrivateKey(key); err != nil {
					fmt.Println(err)
				} else {
					if err := pem.Encode(f, &pem.Block{
						Type:  "EC PRIVATE KEY",
						Bytes: m,
					}); err != nil {
						fmt.Println(err)
					}
				}
			}
		}
	}
	if data, err := ioutil.ReadFile(key); err != nil {
		fmt.Println(err)
	} else {
		if signer, err := ssh.ParsePrivateKey(data); err != nil {
			fmt.Println(err)
		} else {
			server := broker.NewServer(id, addr, backend, signer)
			server.Serve()
		}
	}
}
