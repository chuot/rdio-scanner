// Copyright Saubeo Solutions All Rights Reserved.
// This source code is proprietary and confidential.

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"time"
)

func CreateSelfSignedCert(config *Config) error {
	config.SslCaCertFile = "ca.crt"
	config.SslCaKeyFile = "ca.key"

	if config.SslCertFile == "" {
		config.SslCertFile = "server.crt"
	}
	if config.SslKeyFile == "" {
		config.SslKeyFile = "server.key"
	}

	ca := &x509.Certificate{
		BasicConstraintsValid: true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		SerialNumber:          big.NewInt(int64(time.Now().Year())),
		Subject:               pkix.Name{CommonName: "Rdio Scanner CA"},
	}

	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return err
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	caPEM := new(bytes.Buffer)
	pem.Encode(caPEM, &pem.Block{Type: "CERTIFICATE", Bytes: caBytes})

	file, err := os.Create(config.GetSslCaCertFilePath())
	if err != nil {
		return err
	}
	file.Write(caPEM.Bytes())
	file.Close()

	caKeyPEM := new(bytes.Buffer)
	pem.Encode(caKeyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})

	file, err = os.Create(config.GetSslCaKeyFilePath())
	if err != nil {
		return err
	}
	file.Write(caKeyPEM.Bytes())
	file.Close()

	cert := &x509.Certificate{
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SerialNumber: big.NewInt(int64(time.Now().Year())),
		Subject:      pkix.Name{CommonName: "Rdio Scanner"},
	}

	certKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return err
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &certKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	certPEM := new(bytes.Buffer)
	pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	file, err = os.Create(config.GetSslCertFilePath())
	if err != nil {
		return err
	}
	file.Write(certPEM.Bytes())
	file.Write(caPEM.Bytes())
	file.Close()

	certKeyPEM := new(bytes.Buffer)
	pem.Encode(certKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certKey),
	})

	file, err = os.Create(config.GetSslKeyFilePath())
	if err != nil {
		return err
	}
	file.Write(certKeyPEM.Bytes())
	file.Close()

	return nil
}
