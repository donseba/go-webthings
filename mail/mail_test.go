package mail

import (
	"context"
	"strings"
	"testing"

	form "github.com/donseba/go-form/v2"
)

func TestCompose(t *testing.T) {
	body, err := Compose("Site <site@example.test>", Message{
		To:      []string{"ada@example.test"},
		Subject: "Hallo wereld",
		Text:    "plain",
		HTML:    "<p>rich</p>",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, part := range []string{"To: ada@example.test", "multipart/alternative", "plain", "<p>rich</p>"} {
		if !strings.Contains(string(body), part) {
			t.Errorf("missing %q in\n%s", part, body)
		}
	}

	_, err = Compose("site@example.test", Message{To: []string{"a@example.test"}, Subject: "x\r\nBcc: b@example.test"})
	if err == nil {
		t.Error("header injection accepted")
	}
}

func TestMemoryAndValid(t *testing.T) {
	var outbox Memory

	_ = outbox.Send(context.Background(), Message{To: []string{"a@example.test"}})

	if len(outbox.Sent()) != 1 {
		t.Fatal("not recorded")
	}

	for address, want := range map[string]bool{
		"ada@example.test":       true,
		"Ada <ada@example.test>": false,
		"ada":                    false,
		"ada@localhost":          false,
	} {
		if Valid(address) != want {
			t.Errorf("Valid(%q) != %v", address, want)
		}
	}
}

func TestComposeHeaders(t *testing.T) {
	body, err := Compose("site@example.test", Message{
		To:      []string{"ada@example.test"},
		Subject: "News",
		Text:    "plain",
		Headers: map[string]string{
			"List-Unsubscribe":      "<https://example.test/unsubscribe?t=abc>",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, part := range []string{
		"List-Unsubscribe: <https://example.test/unsubscribe?t=abc>\r\n",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n",
	} {
		if !strings.Contains(string(body), part) {
			t.Errorf("missing %q in\n%s", part, body)
		}
	}

	refused := []map[string]string{
		{"X-Note": "a\r\nBcc: b@example.test"},
		{"Bcc": "b@example.test"},
		{"content-type": "text/html"},
		{"Bad Name": "x"},
		{"Bad:Name": "x"},
		{"": "x"},
	}

	for _, headers := range refused {
		_, err := Compose("site@example.test", Message{To: []string{"a@example.test"}, Text: "x", Headers: headers})
		if err == nil {
			t.Errorf("accepted %q", headers)
		}
	}
}

// TestFormRule checks the go-form validation of addresses: one message at
// the field for an address without a dot in its domain, none for an address
// go-form refuses itself, for an empty value or for a valid address.
func TestFormRule(t *testing.T) {
	type input struct {
		Email   string  `form:"input,email" name:"email"   validate:"mail"`
		Contact string  `form:"input,text"  name:"contact" validate:"mail"`
		Backup  *string `form:"input,email" name:"backup"  validate:"mail"`
	}

	forms := form.NewForm()
	forms.RegisterLocalizedValidation(Rule, FormRule)

	local := "ada@localhost"

	cases := []struct {
		in   input
		want []string
	}{
		{input{Email: "ada@example.test", Contact: "ada@example.test"}, nil},
		{input{}, nil},
		{input{Email: "ada@localhost"}, []string{"email"}},
		{input{Contact: "ada@localhost"}, []string{"contact"}},
		{input{Backup: &local}, []string{"backup"}},
		// go-form's own check refuses a malformed address of an email
		// input; a text input gets the rule's message.
		{input{Email: "not an address", Contact: "not an address"}, []string{"email", "contact"}},
	}

	for _, c := range cases {
		var fields []string

		for _, fieldError := range forms.ValidateForm(&c.in) {
			field, message := fieldError.FieldError()
			if message != "Enter a valid email address." {
				t.Errorf("%+v: message %q", c.in, message)
			}

			fields = append(fields, field)
		}

		if strings.Join(fields, " ") != strings.Join(c.want, " ") {
			t.Errorf("%+v: errors at %q, want %q", c.in, fields, c.want)
		}
	}
}

// TestDescribe checks that the built-in senders say how they send, and
// that SMTP leaves its credentials out.
func TestDescribe(t *testing.T) {
	smtp := SMTP{Host: "mail.example.com", Username: "site", Password: "secret-password", From: "Site <noreply@example.com>"}

	cases := map[string]Describer{
		"SMTP mail.example.com:587, from Site <noreply@example.com>": smtp,
		"SMTP [::1]:2525, from a@example.com":                        SMTP{Host: "::1", Port: 2525, From: "a@example.com"},
		"the server log, not sent":                                   Log{},
		"memory, for tests, not sent":                                &Memory{},
	}

	for want, sender := range cases {
		if got := sender.Describe(); got != want {
			t.Errorf("Describe() = %q, want %q", got, want)
		}
	}

	if description := smtp.Describe(); strings.Contains(description, "secret-password") ||
		strings.Contains(description, "site,") {
		t.Fatalf("the description shows the credentials: %q", description)
	}
}
