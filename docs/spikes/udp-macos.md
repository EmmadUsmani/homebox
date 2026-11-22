# UDP and ICMP egress on macOS

Test host: Apple Silicon, macOS 15.5, Lima 2.3.0, Go 1.27.4. The spike
uses a throwaway Ubuntu 24.04 arm64 VM with a separate Lima home and
a standalone Go module in [spikes/udp](../../spikes/udp).

The second VZ NIC carried public IPv4 TCP, UDP and ICMP echo on this
Mac. Keep Lima's built-in NIC under Seatbelt for SSH and publishing.
Before shipping socket egress, confine the userspace gateway, bound
its resource use and verify the migration on existing guests.

## A. Second NIC and Seatbelt

**Verified (Lima source).** At v2.3.0, `attachNetwork` always initializes
the first NIC. With no named user-v2 network, it uses the instance's
in-process network. A named user-v2 network can replace that backend.
An unmanaged `networks[].socket` entry is appended as a separate virtio
NIC. The existing YAML has no switch that removes the first NIC and
promotes an unmanaged socket into that role.
[Network attachment source](https://github.com/lima-vm/lima/blob/v2.3.0/pkg/driver/vz/vm_darwin.go#L489)

**Verified (Lima source).** The hostagent dials the configured pathname
as an AF_UNIX stream. In both directions, the wire format is a four-byte
unsigned big-endian Ethernet frame length followed by that many raw
Ethernet bytes. The stream begins with frames, without a negotiation
message. The hostagent bridges that stream to a connected Unix datagram
socketpair, one Ethernet frame per datagram. The other end is handed to
`VZFileHandleNetworkDeviceAttachment`, so Apple's VM process consumes
the datagram endpoint. The built-in NIC has a different setup path that
uses descriptor passing to its backend's control socket.
[Framing and socket source](https://github.com/lima-vm/lima/blob/v2.3.0/pkg/driver/vz/network_darwin.go#L53)

**Verified (Mac).** With the existing phase 0 allowlist and the daemon
listening outside the instance directory, `sandbox-exec -f without.sb
limactl start --tty=false udp` failed. The hostagent logged
`dial unix /private/tmp/hb-udp/net.sock: connect: operation not permitted`.
Adding the single exact path below to `allow network-outbound` let VZ
report `running` and the gateway log `Lima connected`. No bind or
IP-network rule was added for the new NIC. The existing instance
subpath had to use the canonical `/private/tmp` spelling on this Mac.

```scheme
(literal "/private/tmp/hb-udp/net.sock")
```

## B. Gateway

**Verified (build and Mac socket attachment).** The prototype uses
gVisor netstack with the gvisor-tap-vsock Ethernet switch, DHCP server
and TCP, UDP and IPv4 echo forwarders. It serves one VM over a socket
in a 0700 directory. It starts no host IP listener. There is no
gateway DNS server or host IP forwarding. DHCP supplies an IPv4 lease
and gateway. Guest DNS should point to a public resolver directly.

The transport handlers check the numeric destination before handing
a new flow to a forwarder. The spike copies `internal/egress`'s policy
and checks current host interface prefixes, failing closed on discovery
errors. The upstream forwarders get an empty address translation map.
DHCP stays inside the userspace stack. A reply generated locally by
the gateway's IP stack, such as an echo reply from its own address,
does not demonstrate reachability of a host service.

**Verified (Mac ICMP datagram).** As a non-root user, a Go probe using
`icmp.ListenPacket("udp4", "0.0.0.0")` and an echo request to
`1.1.1.1` received `peer=1.1.1.1:0 type=echo reply`. The kernel
preserved echo ID 9810 and sequence 1. The same probe under the
hostagent profile failed at bind with `operation not permitted`.
In the Go ICMP API, `udp4` selects an ICMP datagram socket here.

## Reproduce

Use an empty scratch directory. Build the prototype, and run the
gateway in a separate terminal before starting Lima.

```sh
mkdir -m 700 /tmp/hb-udp
mkdir -m 700 /tmp/hb-udp/home
(cd spikes/udp && go build -o /tmp/hb-udp/gateway .)
/tmp/hb-udp/gateway -socket /private/tmp/hb-udp/net.sock
```

Use the [phase 0 YAML](../../spikes/phase0/lima.yaml) with SSH port
32240 and the one static HTTP port changed to 14000. Append:

```yaml
networks:
- socket: /private/tmp/hb-udp/net.sock
  interface: lima0
  metric: 100
  macAddress: "52:55:0a:72:00:15"
```

Generate the profile from
[hostagent.sb](../../spikes/phase0/hostagent.sb), with instance path
`/private/tmp/hb-udp/lima/udp`, SSH port 32240, HTTP port 14000 and
its bind host `localhost`. Add the literal rule above. Port 23040
is the loopback CONNECT proxy allowance for the performance control.

```sh
export HOME=/tmp/hb-udp/home LIMA_HOME=/tmp/hb-udp/lima
limactl create --tty=false --name=udp /tmp/hb-udp/vm.yaml
sandbox-exec -f /tmp/hb-udp/with.sb "$(command -v limactl)" start --tty=false udp
limactl shell udp -- ip -4 route
```

## C. Guest egress and denials

**Verified (guest).** `ip -4 route` showed `default via 10.114.0.1 dev
lima0 metric 100` and `default via 192.168.5.2 dev eth0 metric 200`.
`ip -4 route get 1.1.1.1` selected `lima0`. SSH via Lima's loopback
forward returned the configured guest user. The DHCP-enabled gateway
booted to SSH after an earlier static-only iteration stalled waiting
for SSH while the second NIC repeatedly sought a DHCP lease.

```sh
ping -c 3 -W 2 1.1.1.1
dig +time=2 +tries=1 @9.9.9.9 example.com A
curl --noproxy '*' -4 -sS -o /dev/null -w '%{http_code}\n' https://1.1.1.1
sudo resolvectl dns lima0 9.9.9.9
sudo resolvectl domain lima0 '~.'
sudo resolvectl default-route eth0 no
getent ahostsv4 example.com
```

Ping received 3/3 replies, averaging 17.123 ms. Dig received NOERROR
in 27 ms. Curl returned HTTP 301. Normal hostname resolution worked
after the runtime resolver changes. A UDP socket sent a 48-byte NTPv4
client request to port 123 at an IPv4 address of `time.cloudflare.com`
and received a 48-byte server reply, mode 4, stratum 3. The guest curl
build lacked HTTP/3. QUIC is **UNVERIFIED**.

**Verified (guest and host controls).** For each target, ran
`ping -c1 -W1 TARGET`, attempted a one-second TCP connect, and sent a
UDP datagram with a one-second timeout waiting for the echo. The LAN
and loopback canaries bound TCP and UDP port 23440. Host controls
received the TCP marker and UDP echo from each canary. Host pings
received replies from the Mac's LAN address and the router. Guest
probes sent to the canaries produced no listener events. The router TCP
probe used port 80. A listening TCP or UDP service on the router is
**UNVERIFIED**. Its destination was denied by the daemon before dialing.

| Guest destination | TCP | UDP | ICMP echo |
| --- | --- | --- | --- |
| Lima gateway `192.168.5.2` (loopback service canary) | Refused | Timed out | Virtual gateway replied |
| Spike gateway `10.114.0.1` | Timed out | Timed out | Virtual gateway replied |
| Mac LAN address | Timed out | Timed out | No reply |
| Router address | Timed out | Timed out | No reply |
| `10.100.50.1`, `172.30.250.1`, `192.168.250.1` | Timed out | Timed out | No reply |
| `100.64.3.1`, `169.254.169.254` | Timed out | Timed out | No reply |

The daemon logged destination denials for TCP, UDP and ICMP for the
Mac LAN address, router and private targets. The virtual gateways
generate their own echo replies. Gateway ping therefore cannot serve
as a host-loopback isolation probe. A literal guest connection to
`127.0.0.1` targets the guest itself.

**Verified (guest route bypass test with benchmark profile).** Forced
a public address through the built-in NIC using
`sudo ip route add 1.1.1.1/32 via 192.168.5.2 dev eth0`. The guest's
direct TCP connect to 443 was refused, ping received no reply, and
`dig +time=1 +tries=1 @1.1.1.1 example.com A` timed out. Removed the
test route afterwards. The benchmark profile still permitted the
explicit CONNECT proxy on 23040.

**Verified (SSH-only profile and daemon outage).** Set
`networkOpts.hostLoopback: false`, removed the outbound allowance
for 23040, and restarted the VM under that profile. SSH still
returned the guest user. The socket NIC still carried public TCP,
DNS and echo. The proxy returned curl exit 7, the built-in gateway
returned NXDOMAIN for `example.com`, and forcing the public address
through `eth0` again failed for all three protocols. After sending
SIGTERM to the socket daemon, SSH still worked, while public TCP,
UDP DNS and echo all timed out through the selected route. Recovery
without restarting Lima is **UNVERIFIED**.

## D. Throughput and CPU

**Verified (same guest, endpoint and payload).** Built the root module
with `go build -o /tmp/hb-udp/box ./cmd/box` and ran its existing
`_vmnet` command with proxy port 23040, DNS port 23140, loopback SSH
relay port 2240 and SSH target 32240. Ran three alternating direct and
proxy downloads with this guest command. The direct run set
`PROXY=""`. The control run used `PROXY=http://192.168.5.2:23040`.
A single public IPv4 address for the hostname was used in all six runs.

```sh
curl --noproxy "" --proxy "$PROXY" -4 --http1.1 \
  --connect-to "speed.cloudflare.com:443:$ENDPOINT:443" \
  -fSs --max-time 90 -o /dev/null \
  -w '%{http_code} %{size_download} %{time_total} %{time_starttransfer} %{speed_download}\n' \
  'https://speed.cloudflare.com/__down?bytes=20000000'
```

All six returned HTTP 200 and downloaded 20,000,000 bytes. CPU time
is the difference between `ps -p PID -o time=` samples taken either
side of each command. Approximate core use divides CPU seconds by
curl wall time.

| Path | Median download | Median first byte | Daemon CPU per transfer | Median hostagent CPU per transfer |
| --- | --- | --- | --- | --- |
| Socket NIC | 27.5 MB/s | 141 ms | 0.27–0.30 s (about 39% of one core) | 0.20 s |
| Built-in NIC and box CONNECT proxy | 31.6 MB/s | 66 ms | 0.03–0.04 s (about 6% of one core) | 0.18 s |

These are short WAN samples. The proxy had about 15% higher median
download throughput in this run. First-byte time includes the remote
server and TLS. LAN-controlled measurements, TCP upload, sustained
rates, UDP throughput, concurrency and iperf3 are **UNVERIFIED**.

**Verified (cleanup).** `limactl delete -f udp` deleted the VM, and
`limactl list` reported no instances in the spike's Lima home. The
canaries and both daemons were stopped and the scratch directory was
removed. No host sudo was used.

## E. Security and rollout

**Verified (source and prototype).** The hostagent's bridge parses
stream frame lengths and moves data between the socket and VZ. Lima's
built-in netstack still parses first-NIC traffic. The new daemon parses
guest Ethernet, ARP, IPv4, DHCP, TCP, UDP and ICMP through the upstream
libraries. Egress policy runs in the daemon before forwarding the
numeric destination. Seatbelt still denies hostagent outbound
connections by default, with its existing instance socket permissions
and the added exact outbound socket allowance. No IP forwarding rule
or privileged helper is needed.

**UNVERIFIED (security under hostile load).** The transport hooks and
live denials demonstrate the ordinary paths. Fuzz malformed frames and
fragmentation and test flow eviction, connection floods, repeated MAC
changes, backpressure and shutdown before trusting the parser. The
spike has no daemon filesystem confinement, bounded global flow table
or rate-limited denial logs. Review upstream ICMP reply correlation and
socket lifetimes. A daemon compromise would inherit the daemon's host
access. The policy checks interface prefixes as flows start. It does
not track policy changes for open flows or detect router NAT reflection
to the host's public WAN address. The spike provides IPv4 only.

**UNVERIFIED (existing guests and Docker).** Existing Ubuntu guests
would need an explicit migration of redsocks, iptables OUTPUT and
Docker PREROUTING redirects, proxy environment and apt/Docker proxy
settings, and resolver state. Otherwise configured clients can
continue attempting to use the old proxy instead of the route.
Docker's ordinary bridge masquerade should work through the new
default route. Verify embedded DNS, pulls, bridge TCP/UDP, and custom
networks whose subnets overlap the gateway. No Docker test ran here.

### Recommended design for box on macOS

1. Keep the built-in per-instance NIC for Lima's SSH and static
   publishing. Keep its hostagent confined. For socket egress, remove
   the proxy and DNS outbound permissions, use `networkOpts.hostLoopback:
   false`, and permit one canonical daemon socket. An optional guest
   route removal can keep `eth0` off the default path. Host-side controls
   must hold even if guest root adds a route back.
2. Have `internal/vm` carry a plain socket attachment value and let
   `internal/vm/lima` render Lima YAML. Have `internal/isolation` arrange
   readiness and teardown with `internal/vmnet` and render the exact
   outbound socket permission via `internal/confine`. Keep the socket
   in an owner-only directory and handle stale sockets and failed
   starts. Test that a VZ VM cannot continue with an unintended route
   if the gateway crashes.
3. Put stack and flow lifecycle in `internal/vmnet`, with
   `internal/egress` as the single policy implementation for both the
   gateway and legacy proxy. Enforce policy where host dials occur,
   after any mapping and before opening sockets. Bound guest-controlled
   state and resource use, and isolate the packet parser from user
   files. Consider a separate dial broker if the design needs enforcement
   independent of parser integrity.
4. Configure routes and public DNS in the guest profile. Remove the
   old proxy settings and redirects as part of enabling this mode.
   Verify rollback and migration on existing Ubuntu guests and Docker.
5. Extend doctor and opt-in VM tests with positive TCP/UDP/echo
   controls, live loopback and LAN canaries, forced first-NIC routes,
   and outage recovery. Measure controlled traffic before making
   performance claims. Obtain owner approval for any change to the
   signed design.
