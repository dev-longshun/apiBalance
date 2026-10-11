package notifier

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const smtpTimeout = 20 * time.Second

// EmailConfig is the SMTP setup used for balance alerts.
// Security is "ssl" (implicit TLS, usually port 465) or "starttls" (usually port 587).
type EmailConfig struct {
	Host     string
	Port     int
	Security string
	User     string
	Password string
	FromName string
	To       string // comma separated
}

type Email struct {
	cfg EmailConfig
	to  []string
}

func NewEmail(cfg EmailConfig) *Email {
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.User = strings.TrimSpace(cfg.User)
	cfg.FromName = strings.TrimSpace(cfg.FromName)
	return &Email{cfg: cfg, to: ParseRecipients(cfg.To)}
}

// ParseRecipients splits a comma / semicolon / whitespace separated address list.
func ParseRecipients(raw string) []string {
	var out []string
	for _, s := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '，' || r == '；' || r == ' ' || r == '\n' || r == '\t'
	}) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (e *Email) IsConfigured() bool {
	return e.cfg.Host != "" && e.cfg.Port > 0 && e.cfg.User != "" && e.cfg.Password != "" && len(e.to) > 0
}

// Send sends a plain-text mail to all recipients. It retries once on failure.
func (e *Email) Send(subject, body string) error {
	if err := e.send(subject, body); err != nil {
		time.Sleep(2 * time.Second)
		return e.send(subject, body)
	}
	return nil
}

func (e *Email) send(subject, body string) error {
	addr := net.JoinHostPort(e.cfg.Host, strconv.Itoa(e.cfg.Port))
	dialer := &net.Dialer{Timeout: smtpTimeout}
	tlsCfg := &tls.Config{ServerName: e.cfg.Host}

	var conn net.Conn
	var err error
	if e.cfg.Security == "starttls" {
		conn, err = dialer.Dial("tcp", addr)
	} else {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	}
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器 %s 失败（检查服务器地址、端口和加密方式是否匹配）: %w", addr, err)
	}
	conn.SetDeadline(time.Now().Add(smtpTimeout))

	c, err := smtp.NewClient(conn, e.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP 握手失败: %w", err)
	}
	defer c.Close()

	if e.cfg.Security == "starttls" {
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS 失败: %w", err)
		}
	}
	if err := c.Auth(smtp.PlainAuth("", e.cfg.User, e.cfg.Password, e.cfg.Host)); err != nil {
		return fmt.Errorf("SMTP 登录被拒绝（检查发件邮箱和授权码；QQ / 163 邮箱要填 SMTP 授权码，不是登录密码）: %w", err)
	}
	if err := c.Mail(e.cfg.User); err != nil {
		return fmt.Errorf("发件地址被拒绝: %w", err)
	}
	for _, rcpt := range e.to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("收件地址 %s 被拒绝: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA 失败: %w", err)
	}
	if _, err := w.Write(e.buildMessage(subject, body)); err != nil {
		w.Close()
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("邮件提交失败: %w", err)
	}
	return c.Quit()
}

func (e *Email) buildMessage(subject, body string) []byte {
	from := mail.Address{Name: e.cfg.FromName, Address: e.cfg.User}

	domain := "localhost"
	if i := strings.LastIndex(e.cfg.User, "@"); i >= 0 {
		domain = e.cfg.User[i+1:]
	}
	rnd := make([]byte, 8)
	rand.Read(rnd)

	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + strings.Join(e.to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.BEncoding.Encode("UTF-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString(fmt.Sprintf("Message-ID: <%d.%s@%s>\r\n", time.Now().UnixNano(), hex.EncodeToString(rnd), domain))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")

	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return []byte(b.String())
}

// subjectPrefix tags the subject with the sender name so mails from different
// deployments (e.g. longjin / relay) are distinguishable in the inbox.
func (e *Email) subjectPrefix() string {
	if e.cfg.FromName == "" {
		return ""
	}
	return "[" + e.cfg.FromName + "] "
}

// beijingTime renders an RFC3339 timestamp in UTC+8; other formats are returned as-is.
func beijingTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05") + "（北京时间）"
}

// SendAlert notifies that a site is below threshold.
func (e *Email) SendAlert(siteName string, balance float64, threshold float64, checkTime string, linkURL string) error {
	subject := fmt.Sprintf("%s余额不足：%s 当前 $%.2f，低于阈值 $%.2f", e.subjectPrefix(), siteName, balance, threshold)

	var b strings.Builder
	b.WriteString("⚠️ 余额不足提醒\n\n")
	b.WriteString(fmt.Sprintf("站点：%s\n当前余额：$%.2f\n触发阈值：$%.2f\n查询时间：%s\n", siteName, balance, threshold, beijingTime(checkTime)))
	if linkURL = strings.TrimSpace(linkURL); isHTTPURL(linkURL) {
		b.WriteString("\n充值链接：" + linkURL + "\n")
	}
	b.WriteString("\n请及时充值，避免服务中断。\n同一站点的每档阈值只提醒一次，余额回到阈值以上后重新计数。\n")

	return e.Send(subject, b.String())
}

// SendTestMessage sends a single attempt (no retry) so the settings page gets fast feedback.
func (e *Email) SendTestMessage() error {
	return e.send(
		e.subjectPrefix()+"测试邮件",
		"✅ UpstreamBalance 测试邮件发送成功！\n\n收到这封邮件说明 SMTP 配置正确，余额跌破阈值时会同时发 Telegram 和邮件。\n",
	)
}
