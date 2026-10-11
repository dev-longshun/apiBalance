package notifier

import (
	"encoding/base64"
	"io"
	"mime"
	"net/mail"
	"reflect"
	"strings"
	"testing"
)

func TestParseRecipients(t *testing.T) {
	got := ParseRecipients(" a@qq.com, b@163.com；c@x.com\n,, ")
	want := []string{"a@qq.com", "b@163.com", "c@x.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseRecipients = %v, want %v", got, want)
	}
}

func TestIsConfigured(t *testing.T) {
	full := EmailConfig{Host: "smtp.qq.com", Port: 465, Security: "ssl", User: "a@qq.com", Password: "x", To: "a@qq.com"}
	if !NewEmail(full).IsConfigured() {
		t.Fatal("full config should be configured")
	}
	noTo := full
	noTo.To = " , "
	if NewEmail(noTo).IsConfigured() {
		t.Fatal("config without recipients should not be configured")
	}
}

func TestBuildMessageDecodes(t *testing.T) {
	e := NewEmail(EmailConfig{User: "a@qq.com", FromName: "longjin 额度监控", To: "a@qq.com,b@163.com"})
	subject := e.subjectPrefix() + "余额不足：叮当猫 当前 $95.20，低于阈值 $100.00"
	body := "第一行\n第二行 " + strings.Repeat("长", 80)

	msg, err := mail.ReadMessage(strings.NewReader(string(e.buildMessage(subject, body))))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	dec := new(mail.AddressParser)
	from, err := dec.Parse(msg.Header.Get("From"))
	if err != nil || from.Name != "longjin 额度监控" || from.Address != "a@qq.com" {
		t.Fatalf("From = %+v, err %v", from, err)
	}
	if got, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject")); got != subject {
		t.Fatalf("Subject = %q, want %q", got, subject)
	}
	if msg.Header.Get("Message-Id") == "" {
		t.Fatal("missing Message-ID")
	}

	raw, _ := io.ReadAll(msg.Body)
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("body line longer than 76 chars: %d", len(line))
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(raw), "\r\n", ""))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if string(decoded) != strings.ReplaceAll(body, "\n", "\r\n") {
		t.Fatalf("body mismatch: %q", decoded)
	}
}

func TestBeijingTime(t *testing.T) {
	if got := beijingTime("2026-10-11T10:10:13Z"); got != "2026-10-11 18:10:13（北京时间）" {
		t.Fatalf("beijingTime = %q", got)
	}
	if got := beijingTime("not-a-time"); got != "not-a-time" {
		t.Fatalf("beijingTime fallback = %q", got)
	}
}
