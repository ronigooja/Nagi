package requestrewrite

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"time"
)

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", &Error{Code: "rewrite_storage"}
	}
	return hex.EncodeToString(b), nil
}

// CA status never assesses OS trust. Trust installation/assessment is a separate
// platform operation, and a locally valid CA is not evidence of trust.
type CAStatus struct {
	State       string    `json:"state"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	NotBefore   time.Time `json:"not_before,omitempty"`
	NotAfter    time.Time `json:"not_after,omitempty"`
	Trust       string    `json:"trust"`
}
type CAStore struct{ ConfigDir string }

const caBundle = "root.pem"

func (c CAStore) Generate(now time.Time) (CAStatus, error) {
	d, e := openPrivate(c.ConfigDir, true, true)
	if e != nil {
		return CAStatus{}, e
	}
	defer d.close()
	unlock, e := d.lock()
	if e != nil {
		return CAStatus{}, e
	}
	defer unlock()
	if _, e = d.read(caBundle, 32<<10); !errors.Is(e, os.ErrNotExist) {
		return CAStatus{}, &Error{Code: "rewrite_ca_exists"}
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return CAStatus{}, &Error{Code: "rewrite_ca_invalid"}
	}
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, e := rand.Int(rand.Reader, limit)
	if e != nil {
		return CAStatus{}, &Error{Code: "rewrite_ca_invalid"}
	}
	serial.Add(serial, big.NewInt(1))
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Nagi Local Request Rewrite CA"}, NotBefore: now.UTC().Add(-5 * time.Minute), NotAfter: now.UTC().AddDate(5, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		return CAStatus{}, &Error{Code: "rewrite_ca_invalid"}
	}
	secret, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return CAStatus{}, &Error{Code: "rewrite_ca_invalid"}
	}
	bundle := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret})...)
	defer func() {
		for i := range secret {
			secret[i] = 0
		}
		for i := range bundle {
			bundle[i] = 0
		}
	}()
	if e = d.write(caBundle, bundle, true); e != nil {
		return CAStatus{}, e
	}
	return c.Status(now)
}

// LoadForChild is sensitive: only the unprivileged rewrite child should call it.
// Never include this return value in diagnostic/export/controller messages.
func (c CAStore) LoadForChild(now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	d, e := openPrivate(c.ConfigDir, true, false)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return nil, nil, &Error{Code: "rewrite_ca_required"}
		}
		return nil, nil, &Error{Code: "rewrite_ca_invalid"}
	}
	defer d.close()
	b, e := d.read(caBundle, 32<<10)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return nil, nil, &Error{Code: "rewrite_ca_required"}
		}
		return nil, nil, &Error{Code: "rewrite_ca_invalid"}
	}
	defer func() {
		for i := range b {
			b[i] = 0
		}
	}()
	certBlock, rest := pem.Decode(b)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, nil, &Error{Code: "rewrite_ca_invalid"}
	}
	keyBlock, rest := pem.Decode(rest)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" || len(rest) != 0 {
		return nil, nil, &Error{Code: "rewrite_ca_invalid"}
	}
	cert, e := x509.ParseCertificate(certBlock.Bytes)
	if e != nil {
		return nil, nil, &Error{Code: "rewrite_ca_invalid"}
	}
	parsed, e := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if e != nil {
		return nil, nil, &Error{Code: "rewrite_ca_invalid"}
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) || cert.CheckSignatureFrom(cert) != nil {
		return nil, nil, &Error{Code: "rewrite_ca_invalid"}
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&key.PublicKey) {
		return nil, nil, &Error{Code: "rewrite_ca_invalid"}
	}
	return cert, key, nil
}

// publicCertificate reads only the leading public PEM block. Ordinary status
// and public export never decode/read the following private key material.
func (c CAStore) publicCertificate(now time.Time) (*x509.Certificate, error) {
	d, e := openPrivate(c.ConfigDir, true, false)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return nil, &Error{Code: "rewrite_ca_required"}
		}
		return nil, &Error{Code: "rewrite_ca_invalid"}
	}
	defer d.close()
	f, e := d.openRegular(caBundle)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return nil, &Error{Code: "rewrite_ca_required"}
		}
		return nil, &Error{Code: "rewrite_ca_invalid"}
	}
	defer f.Close()
	var buf bytes.Buffer
	one := make([]byte, 1)
	marker := []byte("-----END CERTIFICATE-----\n")
	for buf.Len() < 16<<10 {
		if _, e = io.ReadFull(f, one); e != nil {
			return nil, &Error{Code: "rewrite_ca_invalid"}
		}
		buf.WriteByte(one[0])
		if bytes.HasSuffix(buf.Bytes(), marker) {
			break
		}
	}
	block, rest := pem.Decode(buf.Bytes())
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		return nil, &Error{Code: "rewrite_ca_invalid"}
	}
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) || cert.CheckSignatureFrom(cert) != nil {
		return nil, &Error{Code: "rewrite_ca_invalid"}
	}
	info, e := f.Stat()
	if e != nil || info.Size() <= int64(buf.Len()+64) {
		return nil, &Error{Code: "rewrite_ca_invalid"}
	}
	return cert, nil
}
func (c CAStore) Status(now time.Time) (CAStatus, error) {
	cert, e := c.publicCertificate(now)
	if e != nil {
		var re *Error
		if errors.As(e, &re) && re.Code == "rewrite_ca_required" {
			return CAStatus{State: "missing", Trust: "unknown"}, nil
		}
		return CAStatus{State: "invalid", Trust: "unknown"}, e
	}
	sum := sha256.Sum256(cert.Raw)
	return CAStatus{State: "generated", Trust: "unknown", Fingerprint: "sha256:" + hex.EncodeToString(sum[:]), NotBefore: cert.NotBefore, NotAfter: cert.NotAfter}, nil
}
func (c CAStore) ExportPublic(now time.Time) ([]byte, error) {
	cert, e := c.publicCertificate(now)
	if e != nil {
		return nil, e
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), nil
}
