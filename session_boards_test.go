package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func TestBoardSessionFactories(t *testing.T) {
	for typ, resource := range map[string]string{"boards": "https://boards.yandex.ru/whiteboard/?hash=fixture", "mtslink": "https://boards.mts-link.ru/board/fixture"} {
		r, e := newProfileSessionRuntime(typ, []string{resource}, "", transport.DefaultConfig(), false, "", "", sessionIdentity{ID: "42", Token: strings.Repeat("a", 32)})
		if e != nil {
			t.Fatal(e)
		}
		if names := r.Transports(); len(names) != 1 || names[0] != typ+"-1" {
			t.Fatal("missing lane")
		}
		r.Stop()
	}
}
func TestOwnedEncryptedBoardSession(t *testing.T) {
	raw := os.Getenv("PAPERFLUX_TEST_BOARD_URL")
	if raw == "" {
		t.Skip("owned board not supplied")
	}
	ident := sessionIdentity{ID: "999", Token: strings.Repeat("fixture-only-", 4), Quiet: true}
	client, e := newProfileSessionRuntime("boards", []string{raw}, "", transport.DefaultConfig(), false, "", "", ident)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Stop()
	exit, e := newProfileSessionRuntime("boards", []string{raw}, "", transport.DefaultConfig(), true, "", "", ident)
	if e != nil {
		t.Fatal(e)
	}
	defer exit.Stop()
	client.session.SetHandshakeTimeout(45 * time.Second)
	exit.session.SetHandshakeTimeout(45 * time.Second)
	got := make(chan []byte, 128)
	client.Receive(func(p []byte) { got <- p })
	exit.Receive(func(p []byte) { _ = exit.Send(p) })
	started := make(chan error, 2)
	go func() { started <- exit.Start() }()
	go func() { started <- client.Start() }()
	for i := 0; i < 2; i++ {
		select {
		case e := <-started:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(50 * time.Second):
			t.Fatal("encrypted Session did not establish")
		}
	}
	for round := 0; round < 3; round++ {
		for i := 0; i < 32; i++ {
			p := bytes.Repeat([]byte{byte(i + 1)}, 1500)
			clear(p[:28])
			p[0] = 0x45
			p[8] = 64
			p[9] = 17
			binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
			copy(p[12:16], []byte{10, 10, 10, 2})
			copy(p[16:20], []byte{10, 10, 10, 1})
			binary.BigEndian.PutUint16(p[20:22], 12345)
			binary.BigEndian.PutUint16(p[22:24], 12346)
			binary.BigEndian.PutUint16(p[24:26], uint16(len(p)-20))
			binary.BigEndian.PutUint32(p[28:32], uint32(round*32+i))
			if e := client.Send(p); e != nil {
				t.Fatal(e)
			}
			select {
			case reply := <-got:
				if !bytes.Equal(reply, p) {
					t.Fatal("echo mismatch")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("encrypted packet not echoed")
			}
		}
		if round == 0 {
			time.Sleep(time.Second)
		}
	}
	t.Log("96 encrypted packets of 1500 bytes echoed; both Session endpoints authenticated")
}
