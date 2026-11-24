package displayproxy

// GuestStart runs as the authenticated guest user. Exit status 42 indicates
// that the Ubuntu desktop layer is missing.
const GuestStart = "test -x /usr/local/lib/homebox-desktop/start || exit 42; exec /usr/local/lib/homebox-desktop/start"

// GuestPort is reserved for the SSH tunnel and cannot be published.
const GuestPort = 8444
