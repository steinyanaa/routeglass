package tlsprovider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/challenge/http01"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/registration"
)

type Status struct {
	Ready    bool      `json:"ready"`
	Provider string    `json:"provider"`
	NotAfter time.Time `json:"not_after,omitempty"`
	RenewAt  time.Time `json:"renew_at,omitempty"`
	Error    string    `json:"error,omitempty"`
}
type Provider interface {
	Load(context.Context) (*tls.Certificate, Status, error)
	Renew(context.Context) error
}
type Files struct{ CertFile, KeyFile string }

func (f *Files) Load(_ context.Context) (*tls.Certificate, Status, error) {
	c, e := tls.LoadX509KeyPair(f.CertFile, f.KeyFile)
	st := Status{Provider: "files"}
	if e != nil {
		st.Error = e.Error()
		return nil, st, e
	}
	leaf, e := x509.ParseCertificate(c.Certificate[0])
	if e != nil {
		st.Error = e.Error()
		return nil, st, e
	}
	c.Leaf = leaf
	st.NotAfter = leaf.NotAfter
	st.RenewAt = leaf.NotAfter.Add(-72 * time.Hour)
	st.Ready = time.Now().Before(leaf.NotAfter)
	if !st.Ready {
		st.Error = "certificate expired"
		return &c, st, errors.New(st.Error)
	}
	return &c, st, nil
}
func (f *Files) Renew(context.Context) error { return nil }

type ACMEConfig struct {
	Mode           string
	Email          string
	Names          []string
	CertFile       string
	KeyFile        string
	AccountKeyFile string
	HTTPAddress    string
	CADirURL       string
	Profile        string
}
type ACME struct{ cfg ACMEConfig }

func NewACME(c ACMEConfig) (*ACME, error) {
	if c.Mode != "acme-ip" && c.Mode != "acme-domain" {
		return nil, errors.New("mode must be acme-ip or acme-domain")
	}
	if len(c.Names) == 0 {
		return nil, errors.New("at least one name is required")
	}
	if c.Mode == "acme-ip" {
		for _, n := range c.Names {
			if _, e := netip.ParseAddr(n); e != nil {
				return nil, fmt.Errorf("acme-ip name %q is not an IP", n)
			}
		}
		if c.Profile == "" {
			c.Profile = "shortlived"
		}
	}
	if c.HTTPAddress == "" {
		c.HTTPAddress = ":80"
	}
	return &ACME{cfg: c}, nil
}
func (a *ACME) Load(ctx context.Context) (*tls.Certificate, Status, error) {
	cert, st, e := (&Files{a.cfg.CertFile, a.cfg.KeyFile}).Load(ctx)
	st.Provider = a.cfg.Mode
	if e != nil {
		return cert, st, e
	}
	if client, _, ce := a.client(ctx); ce == nil && cert.Leaf != nil {
		if ri, re := client.Certificate.GetRenewalInfo(ctx, cert.Leaf); re == nil {
			if at := ri.ShouldRenewAt(time.Now(), 365*24*time.Hour); at != nil {
				st.RenewAt = *at
			}
		}
	}
	return cert, st, nil
}
func (a *ACME) Renew(ctx context.Context) error {
	client, _, e := a.client(ctx)
	if e != nil {
		return e
	}
	provider := http01.NewProviderServerWithOptions(http01.Options{Address: a.cfg.HTTPAddress})
	if e = client.Challenge.SetHTTP01Provider(provider); e != nil {
		return e
	}
	res, e := client.Certificate.Obtain(ctx, certificate.ObtainRequest{Domains: a.cfg.Names, Bundle: true, Profile: a.cfg.Profile})
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(a.cfg.CertFile), 0700); e != nil {
		return e
	}
	if e = atomicWrite(a.cfg.CertFile, res.Certificate, 0644); e != nil {
		return e
	}
	return atomicWrite(a.cfg.KeyFile, res.PrivateKey, 0600)
}
func (a *ACME) client(ctx context.Context) (*lego.Client, *acmeUser, error) {
	key, e := loadOrCreateRSA(a.cfg.AccountKeyFile)
	if e != nil {
		return nil, nil, e
	}
	u := &acmeUser{email: a.cfg.Email, key: key}
	conf := lego.NewConfig(u)
	if a.cfg.CADirURL != "" {
		conf.CADirURL = a.cfg.CADirURL
	}
	client, e := lego.NewClient(conf)
	if e != nil {
		return nil, nil, e
	}
	reg, e := client.Registration.Register(ctx, registration.RegisterOptions{TermsOfServiceAgreed: true})
	if e != nil {
		return nil, nil, e
	}
	u.reg = reg
	return client, u, nil
}

type acmeUser struct {
	email string
	reg   *acme.ExtendedAccount
	key   crypto.Signer
}

func (u *acmeUser) GetEmail() string                       { return u.email }
func (u *acmeUser) GetRegistration() *acme.ExtendedAccount { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.Signer           { return u.key }
func loadOrCreateRSA(path string) (crypto.Signer, error) {
	if b, e := os.ReadFile(path); e == nil {
		block, _ := pem.Decode(b)
		if block == nil {
			return nil, errors.New("invalid account key PEM")
		}
		k, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return nil, e
		}
		signer, ok := k.(crypto.Signer)
		if !ok {
			return nil, errors.New("account key is not a signer")
		}
		return signer, nil
	}
	k, e := rsa.GenerateKey(rand.Reader, 3072)
	if e != nil {
		return nil, e
	}
	b, e := x509.MarshalPKCS8PrivateKey(k)
	if e != nil {
		return nil, e
	}
	if e = atomicWrite(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}), 0600); e != nil {
		return nil, e
	}
	return k, nil
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	tmp := path + ".tmp"
	if e := os.WriteFile(tmp, b, mode); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
