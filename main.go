package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/gliderlabs/ssh"
	"github.com/kr/pty"
)

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

func main() {
	bind := flag.String("bind", ":2020", "host:port bind combination")
	flag.Parse()

	if err := genKey("id_rsa"); err != nil {
		log.Fatalf("error while generating ssh key: %s", err)
	}

	ssh.Handle(func(s ssh.Session) {
		log.Printf("new connection from %v", s.RemoteAddr())

		cmd := exec.Command("bash", "roll.sh")
		ptyReq, winCh, isPty := s.Pty()
		if !isPty {
			s.Exit(1)
			log.Printf("closing non-tty request from %v", s.RemoteAddr())
			return
		}

		defer log.Printf("connection to %v closed", s.RemoteAddr())

		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		f, err := pty.Start(cmd)
		if err != nil {
			panic(err)
		}

		go func() {
			for win := range winCh {
				setWinsize(f, win.Width, win.Height)
			}
		}()

		// go func() {
		// 	io.Copy(f, s) // Stdin.
		// }()
		go func() {
			buf := make([]byte, 32*1024)
			var err error
			for {
				_, err = s.Read(buf)
				if err != nil {
					return
				}

				for _, c := range buf {
					// ^C or ^Z.
					if c == 0x3 || c == 0x1a {
						// Try and clean their terminal by sending ^C to the script.
						// roll.sh *should* run a "reset".
						f.Write([]byte{0x3})

						return
					}
				}
			}
		}()

		io.Copy(s, f) // Stdout.
	})

	log.Printf("starting ssh server on %v...", *bind)
	log.Fatalf("error starting ssh server: %s", ssh.ListenAndServe(*bind, nil, ssh.HostKeyFile("id_rsa")))
}

func genKey(privKeyPath string) error {
	if _, err := os.Stat(privKeyPath); !os.IsNotExist(err) {
		return err
	}

	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return err
	}

	privKeyFile, err := os.Create(privKeyPath)
	if err != nil {
		return err
	}
	defer privKeyFile.Close()

	privKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	if err = pem.Encode(privKeyFile, privKeyPEM); err != nil {
		return err
	}

	return nil
}
