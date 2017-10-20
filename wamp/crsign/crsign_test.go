package crsign

import "testing"

func TestCRSign(t *testing.T) {
	chStr := "{ \"nonce\":\"LHRTC9zeOIrt_9U3\", \"authprovider\":\"userdb\", \"authid\":\"peter\", \"timestamp\":\"2014-06-22T16:36:25.448Z\", \"authrole\":\"user\", \"authmethod\":\"wampcra\", \"session\":3251278072152162 }"

	sig := SignChallenge(chStr, []byte("secret"))
	if sig != "NWktSrMd4ItBSAKYEwvu1bTY7G/sSyjKbz+pNP9c04A=" {
		t.Fatal("wrong signature")
	}
}
