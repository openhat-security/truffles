package cli

import "testing"

func TestIsGcloudServiceAccount(t *testing.T) {
	sas := []string{
		"firebase-adminsdk-fsvv0@otw-production-91ffb.iam.gserviceaccount.com",
		"843002370442-compute@developer.gserviceaccount.com",
	}
	for _, a := range sas {
		if !isGcloudServiceAccount(a) {
			t.Fatalf("%s should be SA", a)
		}
	}
	if isGcloudServiceAccount("adam@freetech.co") {
		t.Fatal("user email should not be SA")
	}
}
