package commands

import "testing"

func TestIsLocalImageReference(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		want     bool
	}{
		{name: "local tagged image", imageRef: "portfolio-mainapp:dd85a3f", want: true},
		{name: "docker hub image", imageRef: "siegwerkportfoliomanagement/skynet-admin-api:latest", want: false},
		{name: "explicit registry image", imageRef: "ghcr.io/example/app:latest", want: false},
		{name: "invalid reference", imageRef: "not a valid image", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isLocalImageReference(test.imageRef); got != test.want {
				t.Fatalf("isLocalImageReference(%q) = %t, want %t", test.imageRef, got, test.want)
			}
		})
	}
}
