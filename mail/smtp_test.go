package mail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// smtpServer accepts one SMTP session without authentication or TLS and
// returns what the client sent: the envelope commands and the message.
func smtpServer(t *testing.T) (string, int, <-chan string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	received := make(chan string, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		var session strings.Builder

		reader := bufio.NewReader(conn)
		reply := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

		reply("220 test")

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}

			command := strings.ToUpper(strings.TrimSpace(line))
			session.WriteString(strings.TrimSpace(line) + "\n")

			switch {
			case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
				reply("250 test")
			case strings.HasPrefix(command, "DATA"):
				reply("354 go on")

				for {
					data, err := reader.ReadString('\n')
					if err != nil || data == ".\r\n" {
						break
					}

					session.WriteString(data)
				}

				reply("250 queued")
			case strings.HasPrefix(command, "QUIT"):
				reply("221 bye")
				received <- session.String()

				return
			default:
				reply("250 ok")
			}
		}

		received <- session.String()
	}()

	host, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)

	return host, number, received
}

func TestSMTP(t *testing.T) {
	host, port, received := smtpServer(t)
	sender := SMTP{Host: host, Port: port, From: "Site <site@example.test>"}

	err := sender.Send(context.Background(), Message{
		To:      []string{"ada@example.test"},
		Subject: "Hallo",
		Text:    "Welkom",
	})
	if err != nil {
		t.Fatal(err)
	}

	session := <-received

	for _, part := range []string{
		"MAIL FROM:<site@example.test>",
		"RCPT TO:<ada@example.test>",
		"From: Site <site@example.test>",
		"Subject: Hallo",
		"Welkom",
	} {
		if !strings.Contains(session, part) {
			t.Errorf("session misses %q:\n%s", part, session)
		}
	}

	if err := sender.Send(context.Background(), Message{Subject: "Nobody"}); err == nil {
		t.Error("a message without recipients was sent")
	}

	if err := (SMTP{Host: host, Port: port, From: "not an address"}).Send(
		context.Background(), Message{To: []string{"ada@example.test"}},
	); err == nil {
		t.Error("an invalid sender was accepted")
	}
}

// hangingServer accepts connections and, when greet is set, sends the
// greeting, then never answers again.
func hangingServer(t *testing.T, greet bool) (string, int) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})

	t.Cleanup(func() {
		close(done)

		_ = listener.Close()
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			if greet {
				_, _ = conn.Write([]byte("220 test\r\n"))
			}

			go func() {
				<-done

				_ = conn.Close()
			}()
		}
	}()

	host, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)

	return host, number
}

func TestSMTPStopsOnContextAndTimeout(t *testing.T) {
	message := Message{To: []string{"ada@example.test"}, Subject: "Hallo", Text: "Welkom"}

	t.Run("cancel", func(t *testing.T) {
		host, port := hangingServer(t, false)
		sender := SMTP{Host: host, Port: port, From: "site@example.test"}

		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)

		start := time.Now()
		err := sender.Send(ctx, message)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}

		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("Send returned after %v", elapsed)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		host, port := hangingServer(t, true)
		sender := SMTP{Host: host, Port: port, From: "site@example.test", Timeout: 200 * time.Millisecond}

		start := time.Now()
		err := sender.Send(context.Background(), message)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want context.DeadlineExceeded, got %v", err)
		}

		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("Send returned after %v", elapsed)
		}
	})
}
