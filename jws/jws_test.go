package jws_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	pdebug "github.com/lestrrat-go/pdebug"
	"github.com/smallstep/jwx/buffer"
	"github.com/smallstep/jwx/internal/ecdsautil"
	"github.com/smallstep/jwx/internal/rsautil"
	"github.com/smallstep/jwx/jwa"
	"github.com/smallstep/jwx/jwk"
	"github.com/smallstep/jwx/jws"
	"github.com/smallstep/jwx/jws/sign"
	"github.com/smallstep/jwx/jws/verify"
	"github.com/stretchr/testify/assert"
)

const examplePayload = `{"iss":"joe",` + "\r\n" + ` "exp":1300819380,` + "\r\n" + ` "http://example.com/is_root":true}`
const exampleCompactSerialization = `eyJ0eXAiOiJKV1QiLA0KICJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ.dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk`

func TestParse(t *testing.T) {
	t.Run("Empty bytes.Buffer", func(t *testing.T) {
		_, err := jws.Parse(&bytes.Buffer{})
		if !assert.Error(t, err, "Parsing an empty buffer should result in an error") {
			return
		}
	})
	t.Run("Compact missing parts", func(t *testing.T) {
		incoming := strings.Join(
			(strings.Split(
				exampleCompactSerialization,
				".",
			))[:2],
			".",
		)
		_, err := jws.ParseString(incoming)
		if !assert.Error(t, err, "Parsing compact serialization with less than 3 parts should be an error") {
			return
		}
	})
	t.Run("Compact bad header", func(t *testing.T) {
		parts := strings.Split(exampleCompactSerialization, ".")
		parts[0] = "%badvalue%"
		incoming := strings.Join(parts, ".")

		_, err := jws.ParseString(incoming)
		if !assert.Error(t, err, "Parsing compact serialization with bad header should be an error") {
			return
		}
	})
	t.Run("Compact bad payload", func(t *testing.T) {
		parts := strings.Split(exampleCompactSerialization, ".")
		parts[1] = "%badvalue%"
		incoming := strings.Join(parts, ".")

		_, err := jws.ParseString(incoming)
		if !assert.Error(t, err, "Parsing compact serialization with bad payload should be an error") {
			return
		}
	})
	t.Run("Compact bad signature", func(t *testing.T) {
		parts := strings.Split(exampleCompactSerialization, ".")
		parts[2] = "%badvalue%"
		incoming := strings.Join(parts, ".")

		t.Logf("incoming = '%s'", incoming)
		_, err := jws.ParseString(incoming)
		if !assert.Error(t, err, "Parsing compact serialization with bad signature should be an error") {
			return
		}
	})
}

func TestRoundtrip(t *testing.T) {
	payload := []byte("Lorem ipsum")
	sharedkey := []byte("Avracadabra")

	hmacAlgorithms := []jwa.SignatureAlgorithm{jwa.HS256, jwa.HS384, jwa.HS512}
	for _, alg := range hmacAlgorithms {
		t.Run("HMAC "+alg.String(), func(t *testing.T) {
			signed, err := jws.Sign(payload, alg, sharedkey)
			if !assert.NoError(t, err, "Sign succeeds") {
				return
			}
			if pdebug.Enabled {
				pdebug.Printf("signed string: %s", signed)
			}

			verified, err := jws.Verify(signed, alg, sharedkey)
			if !assert.NoError(t, err, "Verify succeeded") {
				return
			}

			if !assert.Equal(t, payload, verified, "verified payload matches") {
				return
			}
		})
	}
	t.Run("HMAC SignMulti", func(t *testing.T) {
		var signed []byte
		t.Run("Sign", func(t *testing.T) {
			var options []jws.Option
			for _, alg := range hmacAlgorithms {
				signer, err := sign.New(alg)
				if !assert.NoError(t, err, `sign.New should succeed`) {
					return
				}
				options = append(options, jws.WithSigner(signer, sharedkey, nil, nil))
			}
			var err error
			signed, err = jws.SignMulti(payload, options...)
			if !assert.NoError(t, err, `jws.SignMulti should succeed`) {
				return
			}
		})
		for _, alg := range hmacAlgorithms {
			t.Run("Verify "+alg.String(), func(t *testing.T) {
				verified, err := jws.Verify(signed, alg, sharedkey)
				if !assert.NoError(t, err, "Verify succeeded") {
					return
				}

				if !assert.Equal(t, payload, verified, "verified payload matches") {
					return
				}
			})
		}
	})
}

func TestVerifyWithJWKSet(t *testing.T) {
	payload := []byte("Hello, World!")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if !assert.NoError(t, err, "RSA key generated") {
		return
	}

	jwkkey, err := jwk.New(&key.PublicKey)
	if !assert.NoError(t, err, "JWK public key generated") {
		return
	}
	jwkkey.Set(jwk.AlgorithmKey, jwa.RS256)

	buf, err := jws.Sign(payload, jwa.RS256, key)
	if !assert.NoError(t, err, "Signature generated successfully") {
		return
	}

	verified, err := jws.VerifyWithJWKSet(buf, &jwk.Set{Keys: []jwk.Key{jwkkey}}, nil)
	if !assert.NoError(t, err, "Verify is successful") {
		return
	}

	verified, err = jws.VerifyWithJWK(buf, jwkkey)
	if !assert.NoError(t, err, "Verify is successful") {
		return
	}

	if !assert.Equal(t, payload, verified, "Verified payload is the same") {
		return
	}
}

func TestRoundtrip_RSACompact(t *testing.T) {
	payload := []byte("Hello, World!")
	for _, alg := range []jwa.SignatureAlgorithm{jwa.RS256, jwa.RS384, jwa.RS512, jwa.PS256, jwa.PS384, jwa.PS512} {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if !assert.NoError(t, err, "RSA key generated") {
			return
		}

		buf, err := jws.Sign(payload, alg, key)
		if !assert.NoError(t, err, "(%s) Signature generated successfully", alg) {
			return
		}

		parsers := map[string]func([]byte) (*jws.Message, error){
			"Parse(io.Reader)": func(b []byte) (*jws.Message, error) { return jws.Parse(bytes.NewReader(b)) },
			"Parse(string)":    func(b []byte) (*jws.Message, error) { return jws.ParseString(string(b)) },
		}
		for name, f := range parsers {
			m, err := f(buf)
			if !assert.NoError(t, err, "(%s) %s is successful", alg, name) {
				return
			}

			if !assert.Equal(t, payload, m.Payload(), "(%s) %s: Payload is decoded", alg, name) {
				return
			}
		}

		verified, err := jws.Verify(buf, alg, &key.PublicKey)
		if !assert.NoError(t, err, "(%s) Verify is successful", alg) {
			return
		}

		if !assert.Equal(t, payload, verified, "(%s) Verified payload is the same", alg) {
			return
		}
	}
}

func TestEncode(t *testing.T) {
	// HS256Compact tests that https://tools.ietf.org/html/rfc7515#appendix-A.1 works
	t.Run("HS256Compact", func(t *testing.T) {
		const hdr = `{"typ":"JWT",` + "\r\n" + ` "alg":"HS256"}`
		const hmacKey = `AyM1SysPpbyDfgZld3umj1qzKObwVMkoqQ-EstJQLr_T-1qS0gZH75aKtMN3Yj0iPS4hcgUuTwjAzZr1Z9CAow`
		const expected = `eyJ0eXAiOiJKV1QiLA0KICJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ.dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk`

		hmacKeyDecoded := buffer.Buffer{}
		hmacKeyDecoded.Base64Decode([]byte(hmacKey))

		hdrbuf, err := buffer.Buffer(hdr).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}
		payload, err := buffer.Buffer(examplePayload).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}

		signingInput := bytes.Join(
			[][]byte{
				hdrbuf,
				payload,
			},
			[]byte{'.'},
		)

		sign, err := sign.New(jwa.HS256)
		if !assert.NoError(t, err, "HMAC signer created successfully") {
			return
		}

		signature, err := sign.Sign(signingInput, hmacKeyDecoded.Bytes())
		if !assert.NoError(t, err, "PayloadSign is successful") {
			return
		}
		sigbuf, err := buffer.Buffer(signature).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}

		encoded := bytes.Join(
			[][]byte{
				signingInput,
				sigbuf,
			},
			[]byte{'.'},
		)
		if !assert.Equal(t, expected, string(encoded), "generated compact serialization should match") {
			return
		}

		msg, err := jws.Parse(bytes.NewReader(encoded))
		if !assert.NoError(t, err, "Parsing compact encoded serialization succeeds") {
			return
		}

		signatures := msg.Signatures()
		if !assert.Len(t, signatures, 1, `there should be exactly one signature`) {
			return
		}

		if !assert.Equal(t, signatures[0].ProtectedHeaders().Algorithm(), jwa.HS256, "Algorithm in header matches") {
			return
		}

		v, err := verify.New(jwa.HS256)
		if !assert.NoError(t, err, "HmacVerify created") {
			return
		}

		if !assert.NoError(t, v.Verify(signingInput, signature, hmacKeyDecoded.Bytes()), "Verify succeeds") {
			return
		}
	})
	t.Run("RS256Compact", func(t *testing.T) {
		// RS256Compact tests that https://tools.ietf.org/html/rfc7515#appendix-A.2 works
		const hdr = `{"alg":"RS256"}`
		const expected = `eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ.cC4hiUPoj9Eetdgtv3hF80EGrhuB__dzERat0XF9g2VtQgr9PJbu3XOiZj5RZmh7AAuHIm4Bh-0Qc_lF5YKt_O8W2Fp5jujGbds9uJdbF9CUAr7t1dnZcAcQjbKBYNX4BAynRFdiuB--f_nZLgrnbyTyWzO75vRK5h6xBArLIARNPvkSjtQBMHlb1L07Qe7K0GarZRmB_eSN9383LcOLn6_dO--xi12jzDwusC-eOkHWEsqtFZESc6BfI7noOPqvhJ1phCnvWh6IeYI2w9QOYEUipUTI8np6LbgGY9Fs98rqVt5AXLIhWkWywlVmtVrBp0igcN_IoypGlUPQGe77Rw`
		const jwksrc = `{
    "kty":"RSA",
    "n":"ofgWCuLjybRlzo0tZWJjNiuSfb4p4fAkd_wWJcyQoTbji9k0l8W26mPddxHmfHQp-Vaw-4qPCJrcS2mJPMEzP1Pt0Bm4d4QlL-yRT-SFd2lZS-pCgNMsD1W_YpRPEwOWvG6b32690r2jZ47soMZo9wGzjb_7OMg0LOL-bSf63kpaSHSXndS5z5rexMdbBYUsLA9e-KXBdQOS-UTo7WTBEMa2R2CapHg665xsmtdVMTBQY4uDZlxvb3qCo5ZwKh9kG4LT6_I5IhlJH7aGhyxXFvUK-DWNmoudF8NAco9_h9iaGNj8q2ethFkMLs91kzk2PAcDTW9gb54h4FRWyuXpoQ",
    "e":"AQAB",
    "d":"Eq5xpGnNCivDflJsRQBXHx1hdR1k6Ulwe2JZD50LpXyWPEAeP88vLNO97IjlA7_GQ5sLKMgvfTeXZx9SE-7YwVol2NXOoAJe46sui395IW_GO-pWJ1O0BkTGoVEn2bKVRUCgu-GjBVaYLU6f3l9kJfFNS3E0QbVdxzubSu3Mkqzjkn439X0M_V51gfpRLI9JYanrC4D4qAdGcopV_0ZHHzQlBjudU2QvXt4ehNYTCBr6XCLQUShb1juUO1ZdiYoFaFQT5Tw8bGUl_x_jTj3ccPDVZFD9pIuhLhBOneufuBiB4cS98l2SR_RQyGWSeWjnczT0QU91p1DhOVRuOopznQ",
    "p":"4BzEEOtIpmVdVEZNCqS7baC4crd0pqnRH_5IB3jw3bcxGn6QLvnEtfdUdiYrqBdss1l58BQ3KhooKeQTa9AB0Hw_Py5PJdTJNPY8cQn7ouZ2KKDcmnPGBY5t7yLc1QlQ5xHdwW1VhvKn-nXqhJTBgIPgtldC-KDV5z-y2XDwGUc",
    "q":"uQPEfgmVtjL0Uyyx88GZFF1fOunH3-7cepKmtH4pxhtCoHqpWmT8YAmZxaewHgHAjLYsp1ZSe7zFYHj7C6ul7TjeLQeZD_YwD66t62wDmpe_HlB-TnBA-njbglfIsRLtXlnDzQkv5dTltRJ11BKBBypeeF6689rjcJIDEz9RWdc",
    "dp":"BwKfV3Akq5_MFZDFZCnW-wzl-CCo83WoZvnLQwCTeDv8uzluRSnm71I3QCLdhrqE2e9YkxvuxdBfpT_PI7Yz-FOKnu1R6HsJeDCjn12Sk3vmAktV2zb34MCdy7cpdTh_YVr7tss2u6vneTwrA86rZtu5Mbr1C1XsmvkxHQAdYo0",
    "dq":"h_96-mK1R_7glhsum81dZxjTnYynPbZpHziZjeeHcXYsXaaMwkOlODsWa7I9xXDoRwbKgB719rrmI2oKr6N3Do9U0ajaHF-NKJnwgjMd2w9cjz3_-kyNlxAr2v4IKhGNpmM5iIgOS1VZnOZ68m6_pbLBSp3nssTdlqvd0tIiTHU",
    "qi":"IYd7DHOhrWvxkwPQsRM2tOgrjbcrfvtQJipd-DlcxyVuuM9sQLdgjVk2oy26F0EmpScGLq2MowX7fhd_QJQ3ydy5cY7YIBi87w93IKLEdfnbJtoOPLUW0ITrJReOgo1cq9SbsxYawBgfp_gh6A5603k2-ZQwVK0JKSHuLFkuQ3U"
  }`

		privkey, err := rsautil.PrivateKeyFromJSON([]byte(jwksrc))
		if !assert.NoError(t, err, "parsing jwk should be successful") {
			return
		}

		sign, err := sign.New(jwa.RS256)
		if !assert.NoError(t, err, "RsaSign created successfully") {
			return
		}

		hdrbuf, err := buffer.Buffer(hdr).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}
		payload, err := buffer.Buffer(examplePayload).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}

		signingInput := bytes.Join(
			[][]byte{
				hdrbuf,
				payload,
			},
			[]byte{'.'},
		)
		signature, err := sign.Sign(signingInput, privkey)
		if !assert.NoError(t, err, "PayloadSign is successful") {
			return
		}
		sigbuf, err := buffer.Buffer(signature).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}

		encoded := bytes.Join(
			[][]byte{
				signingInput,
				sigbuf,
			},
			[]byte{'.'},
		)

		if !assert.Equal(t, expected, string(encoded), "generated compact serialization should match") {
			return
		}

		msg, err := jws.Parse(bytes.NewReader(encoded))
		if !assert.NoError(t, err, "Parsing compact encoded serialization succeeds") {
			return
		}

		signatures := msg.Signatures()
		if !assert.Len(t, signatures, 1, `there should be exactly one signature`) {
			return
		}

		if !assert.Equal(t, signatures[0].ProtectedHeaders().Algorithm(), jwa.RS256, "Algorithm in header matches") {
			return
		}

		v, err := verify.New(jwa.RS256)
		if !assert.NoError(t, err, "Verify created") {
			return
		}

		if !assert.NoError(t, v.Verify(signingInput, signature, &privkey.PublicKey), "Verify succeeds") {
			return
		}
	})
	t.Run("ES256Compact", func(t *testing.T) {
		// ES256Compact tests that https://tools.ietf.org/html/rfc7515#appendix-A.3 works
		const hdr = `{"alg":"ES256"}`
		const jwksrc = `{
    "kty":"EC",
    "crv":"P-256",
    "x":"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU",
    "y":"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0",
    "d":"jpsQnnGQmL-YBIffH1136cspYG6-0iY7X1fCE9-E9LI"
  }`

		privkey, err := ecdsautil.PrivateKeyFromJSON([]byte(jwksrc))
		if !assert.NoError(t, err, "parsing jwk should be successful") {
			return
		}

		signer, err := sign.New(jwa.ES256)
		if !assert.NoError(t, err, "RsaSign created successfully") {
			return
		}

		hdrbuf, err := buffer.Buffer(hdr).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}
		payload, err := buffer.Buffer(examplePayload).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}

		signingInput := bytes.Join(
			[][]byte{
				hdrbuf,
				payload,
			},
			[]byte{'.'},
		)
		signature, err := signer.Sign(signingInput, privkey)
		if !assert.NoError(t, err, "PayloadSign is successful") {
			return
		}
		sigbuf, err := buffer.Buffer(signature).Base64Encode()
		if !assert.NoError(t, err, "base64 encode successful") {
			return
		}

		encoded := bytes.Join(
			[][]byte{
				signingInput,
				sigbuf,
			},
			[]byte{'.'},
		)

		// The signature contains random factor, so unfortunately we can't match
		// the output against a fixed expected outcome. We'll wave doing an
		// exact match, and just try to verify using the signature

		msg, err := jws.Parse(bytes.NewReader(encoded))
		if !assert.NoError(t, err, "Parsing compact encoded serialization succeeds") {
			return
		}

		signatures := msg.Signatures()
		if !assert.Len(t, signatures, 1, `there should be exactly one signature`) {
			return
		}

		if !assert.Equal(t, signatures[0].ProtectedHeaders().Algorithm(), jwa.ES256, "Algorithm in header matches") {
			return
		}

		v, err := verify.New(jwa.ES256)
		if !assert.NoError(t, err, "EcdsaVerify created") {
			return
		}
		if !assert.NoError(t, v.Verify(signingInput, signature, &privkey.PublicKey), "Verify succeeds") {
			return
		}
	})
	t.Run("UnsecuredCompact", func(t *testing.T) {
		s := `eyJhbGciOiJub25lIn0.eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ.`

		m, err := jws.Parse(strings.NewReader(s))
		if !assert.NoError(t, err, "Parsing compact serialization") {
			return
		}

		{
			v := map[string]interface{}{}
			if !assert.NoError(t, json.Unmarshal(m.Payload(), &v), "Unmarshal payload") {
				return
			}
			if !assert.Equal(t, v["iss"], "joe", "iss matches") {
				return
			}
			if !assert.Equal(t, int(v["exp"].(float64)), 1300819380, "exp matches") {
				return
			}
			if !assert.Equal(t, v["http://example.com/is_root"], true, "'http://example.com/is_root' matches") {
				return
			}
		}

		if !assert.Len(t, m.Signatures(), 1, "There should be 1 signature") {
			return
		}

		signatures := m.Signatures()
		if !assert.Equal(t, signatures[0].ProtectedHeaders().Algorithm(), jwa.NoSignature, "Algorithm = 'none'") {
			return
		}
		if !assert.Empty(t, signatures[0].Signature(), "Signature should be empty") {
			return
		}
	})
	t.Run("CompleteJSON", func(t *testing.T) {
		s := `{
    "payload": "eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ",
    "signatures":[
      {
        "header": {"kid":"2010-12-29"},
        "protected":"eyJhbGciOiJSUzI1NiJ9",
        "signature": "cC4hiUPoj9Eetdgtv3hF80EGrhuB__dzERat0XF9g2VtQgr9PJbu3XOiZj5RZmh7AAuHIm4Bh-0Qc_lF5YKt_O8W2Fp5jujGbds9uJdbF9CUAr7t1dnZcAcQjbKBYNX4BAynRFdiuB--f_nZLgrnbyTyWzO75vRK5h6xBArLIARNPvkSjtQBMHlb1L07Qe7K0GarZRmB_eSN9383LcOLn6_dO--xi12jzDwusC-eOkHWEsqtFZESc6BfI7noOPqvhJ1phCnvWh6IeYI2w9QOYEUipUTI8np6LbgGY9Fs98rqVt5AXLIhWkWywlVmtVrBp0igcN_IoypGlUPQGe77Rw"
      },
      {
        "header": {"kid":"e9bc097a-ce51-4036-9562-d2ade882db0d"},
        "protected":"eyJhbGciOiJFUzI1NiJ9",
        "signature": "DtEhU3ljbEg8L38VWAfUAqOyKAM6-Xx-F4GawxaepmXFCgfTjDxw5djxLa8ISlSApmWQxfKTUJqPP3-Kg6NU1Q"
      }
    ]
  }`

		m, err := jws.Parse(strings.NewReader(s))
		if !assert.NoError(t, err, "Unmarshal complete json serialization") {
			return
		}

		if !assert.Len(t, m.Signatures(), 2, "There should be 2 signatures") {
			return
		}

		var sigs []*jws.Signature
		sigs = m.LookupSignature("2010-12-29")
		if !assert.Len(t, sigs, 1, "There should be 1 signature with kid = '2010-12-29'") {
			return
		}

		jsonbuf, err := json.Marshal(m)
		if !assert.NoError(t, err, "Marshal JSON is successful") {
			return
		}

		b := &bytes.Buffer{}
		json.Compact(b, jsonbuf)

		if !assert.Equal(t, b.Bytes(), jsonbuf, "generated json matches") {
			return
		}
	})
	t.Run("FlattenedJSON", func(t *testing.T) {
		s := `{
    "payload": "eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ",
    "protected":"eyJhbGciOiJFUzI1NiJ9",
    "header": {
      "kid":"e9bc097a-ce51-4036-9562-d2ade882db0d"
    },
    "signature": "DtEhU3ljbEg8L38VWAfUAqOyKAM6-Xx-F4GawxaepmXFCgfTjDxw5djxLa8ISlSApmWQxfKTUJqPP3-Kg6NU1Q"
  }`

		m, err := jws.Parse(strings.NewReader(s))
		if !assert.NoError(t, err, "Parsing flattened json serialization") {
			return
		}

		if !assert.Len(t, m.Signatures(), 1, "There should be 1 signature") {
			return
		}

		jsonbuf, _ := json.MarshalIndent(m, "", "  ")
		t.Logf("%s", jsonbuf)
	})
}

/*
func TestSign_HeaderValues(t *testing.T) {
	const jwksrc = `{
    "kty":"EC",
    "crv":"P-256",
    "x":"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU",
    "y":"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0",
    "d":"jpsQnnGQmL-YBIffH1136cspYG6-0iY7X1fCE9-E9LI"
  }`

	privkey, err := ecdsautil.PrivateKeyFromJSON([]byte(jwksrc))
	if !assert.NoError(t, err, "parsing jwk should be successful") {
		return
	}

	payload := []byte("Hello, World!")

	hdr := jws.NewHeader()
	hdr.KeyID = "helloworld01"
	encoded, err := jws.Sign(payload, jwa.ES256, privkey, jws.WithPublicHeaders(hdr))
	if !assert.NoError(t, err, "Sign should succeed") {
		return
	}

	// Although we set KeyID to the public header, in compact serialization
	// there's no difference
	msg, err := jws.Parse(bytes.NewReader(encoded))
	if !assert.NoError(t, err, `parse should succeed`) {
		return
	}

	if !assert.Equal(t, hdr.KeyID, msg.Signatures[0].ProtectedHeader.KeyID, "KeyID should match") {
		return
	}

	verified, err := jws.Verify(encoded, jwa.ES256, &privkey.PublicKey)
	if !assert.NoError(t, err, "Verify should succeed") {
		return
	}
	if !assert.Equal(t, verified, payload, "Payload should match") {
		return
	}
}
*/

func TestPublicHeaders(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if !assert.NoError(t, err, "GenerateKey should succeed") {
		return
	}

	signer, err := sign.New(jwa.RS256)
	if !assert.NoError(t, err, "rsasign.NewSigner should succeed") {
		return
	}
	_ = signer // TODO

	pubkey := key.PublicKey
	pubjwk, err := jwk.New(&pubkey)
	if !assert.NoError(t, err, "NewRsaPublicKey should succeed") {
		return
	}
	_ = pubjwk // TODO

	/*
		if !assert.NoError(t, signer.PublicHeaders().Set("jwk", pubjwk), "Set('jwk') should succeed") {
			return
		}
	*/
}

func TestDecode_ES384Compact_NoSigTrim(t *testing.T) {
	incoming := "eyJhbGciOiJFUzM4NCIsInR5cCI6IkpXVCIsImtpZCI6IjE5MzFmZTQ0YmFhMWNhZTkyZWUzNzYzOTQ0MDU1OGMwODdlMTRlNjk5ZWU5NjVhM2Q1OGU1MmU2NGY4MDE0NWIifQ.eyJpc3MiOiJicmt0LWNsaS0xLjAuN3ByZTEiLCJpYXQiOjE0ODQ2OTU1MjAsImp0aSI6IjgxYjczY2Y3In0.DdFi0KmPHSv4PfIMGcWGMSRLmZsfRPQ3muLFW6Ly2HpiLFFQWZ0VEanyrFV263wjlp3udfedgw_vrBLz3XC8CkbvCo_xeHMzaTr_yfhjoheSj8gWRLwB-22rOnUX_M0A"
	t.Logf("incoming = '%s'", incoming)
	const jwksrc = `{
    "kty":"EC",
    "crv":"P-384",
    "x":"YHVZ4gc1RDoqxKm4NzaN_Y1r7R7h3RM3JMteC478apSKUiLVb4UNytqWaLoE6ygH",
    "y":"CRKSqP-aYTIsqJfg_wZEEYUayUR5JhZaS2m4NLk2t1DfXZgfApAJ2lBO0vWKnUMp"
  }`

	pubkey, err := ecdsautil.PublicKeyFromJSON([]byte(jwksrc))
	if !assert.NoError(t, err, "parsing jwk should be successful") {
		return
	}
	v, err := verify.New(jwa.ES384)
	if !assert.NoError(t, err, "EcdsaVerify created") {
		return
	}

	protected, payload, signature, err := jws.SplitCompact(strings.NewReader(incoming))
	if !assert.NoError(t, err, `jws.SplitCompact shoud succeed`) {
		return
	}

	var buf bytes.Buffer
	buf.Write(protected)
	buf.WriteByte('.')
	buf.Write(payload)

	decodedSignature := make([]byte, base64.RawURLEncoding.DecodedLen(len(signature)))
	if _, err := base64.RawURLEncoding.Decode(decodedSignature, signature); !assert.NoError(t, err, `decoding signature should succeed`) {
		return
	}

	if !assert.NoError(t, v.Verify(buf.Bytes(), decodedSignature, pubkey), "Verify succeeds") {
		return
	}
}
