package tools

import "testing"

func TestChecksumForAsset(t *testing.T) {
	const asset = "deno-x86_64-pc-windows-msvc.zip"
	const sum = "a0c3101b4158d1dfb7d6a78a7bf0f3de80c96bb423c152beec8beb22786f2238"

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "standard list",
			body: sum + "  " + asset + "\n",
			want: sum,
		},
		{
			name: "Windows Get-FileHash",
			body: "\r\nAlgorithm : SHA256\r\n" +
				"Hash      : A0C3101B4158D1DFB7D6A78A7BF0F3DE80C96BB423C152BEEC8BEB22786F2238\r\n" +
				"Path      : C:\\a\\deno\\deno\\target\\release\\" + asset + "\r\n",
			want: sum,
		},
		{
			name: "wrong Windows asset",
			body: "Algorithm : SHA256\nHash : " + sum + "\nPath : C:\\release\\other.zip\n",
		},
		{
			name: "invalid hash",
			body: "Algorithm : SHA256\nHash : algorithm\nPath : C:\\release\\" + asset + "\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checksumForAsset([]byte(tc.body), asset)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("checksum = %q, want error", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("checksum = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
