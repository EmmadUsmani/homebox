# Phase 0 macOS Lima spike

On Apple M2 Pro, macOS 15.5, Lima 2.3.0 and Ubuntu 24.04 arm64, Seatbelt blocked the tested direct connections. Proxied internet and keyed SSH via a LAN relay worked. The LAN client was the Mac itself. The Phase 0 exit criterion still requires a test from the MacBook.

## A. Sandboxed VZ and lifecycle

The [Lima YAML](../../spikes/phase0/lima.yaml) and [Seatbelt template](../../spikes/phase0/hostagent.sb) booted a fresh 2 CPU, 2 GiB, 20 GiB VM:

```sh
export LIMA_HOME="$HOME/.homebox-spike/lima"
limactl create --tty=false --name=spike-final spikes/phase0/lima.yaml
sed -e "s|@INSTANCE_DIR@|$LIMA_HOME/spike-final|g" \
    -e 's/@SSH_PORT@/2201/g' -e 's/@PROXY_PORT@/23128/g' \
    -e 's/@PUBLISH_HOST@/localhost/g' -e 's/@HTTP_PORT@/20100/g' \
    spikes/phase0/hostagent.sb > "$HOME/.homebox-spike/hostagent.sb"
sandbox-exec -f "$HOME/.homebox-spike/hostagent.sb" /opt/homebrew/bin/limactl start --tty=false spike-final
limactl shell spike-final -- id -un
```

SSH returned `homebox`. A stop and sandboxed restart worked. During one stop, Lima was still waiting after about 30 seconds. `limactl shell spike-homebox -- sudo systemctl poweroff` completed shutdown. Hostagent logs showed `operation not permitted` on a direct DNS dial and on denied binds. `log show --last 5m --predicate 'sender == "Sandbox" AND eventMessage CONTAINS "limactl"'` gave no matching denials. The checked-in profile worked with two TCP outbound exceptions, two TCP listeners and instance-local socket allowances. Absolute minimum per socket and a separate process sandbox-status query are UNVERIFIED.

## B. Direct public, LAN and loopback access

All four guest probes returned curl exit 7 and HTTP 000 in under 1 ms. Host controls reached both temporary HTTP listeners at port 23129, one on loopback and one on the LAN address. The router's HTTP endpoint returned 302 from the host.

```sh
curl --noproxy '*' -s -m 5 https://1.1.1.1
curl --noproxy '*' -s -m 5 http://<host-lan-ip>:23129
curl --noproxy '*' -s -m 5 http://<router-ip>
curl --noproxy '*' -s -m 5 http://192.168.5.2:23129
```

The guest reached the proxy on `192.168.5.2:23128`. IPv6, alternate LAN interfaces and public-address NAT hairpin routes are UNVERIFIED.

## C. DNS

With `hostResolver.enabled: true`, the narrow profile blocked `listen udp4 127.0.0.1:0`. Allowing wildcard loopback UDP binds exposed a second failure at `listen tcp4 127.0.0.1:0`. Seatbelt rejected `localhost:0` at parse time. With both wildcard loopback bind allowances the VM booted, but `getent ahostsv4 example.com` still exited 2. The resolver's mDNSResponder versus Go path is UNVERIFIED.

With the host resolver disabled, guest DNS via the gateway returned NXDOMAIN and direct UDP to `1.1.1.1:53` timed out. Explicit proxy clients delegated resolution to the proxy. For transparent clients, the [UDP relay](../../spikes/phase0/dns-relay.js) listened on host loopback 23553 and sent DNS to the fixed public resolver `1.1.1.1:53`. Add `(remote udp "localhost:23553")` to the Seatbelt outbound allow stanza and run:

```sh
# Host
node spikes/phase0/dns-relay.js
# Guest
dig +time=2 +tries=1 @192.168.5.2 -p 23553 example.com A
sudo resolvectl dns eth0 192.168.5.2:23553
sudo resolvectl domain eth0 '~.'
getent ahostsv4 example.com
```

`dig` returned NOERROR and `getent` succeeded. DNS TCP fallback, persistence and Docker DNS on the nonstandard port are UNVERIFIED.

## D. Explicit proxy

```sh
# Host, in separate terminals for proxy and Lima
node spikes/phase0/proxy.js
limactl shell spike-final -- sudo sh -s < spikes/phase0/guest-proxy.sh
# Guest
https_proxy=http://192.168.5.2:23128 curl -fsS https://example.com -o /dev/null
https_proxy=http://192.168.5.2:23128 curl -fsS https://<host-lan-ip>:23129
sudo apt-get update
sudo apt-get install -y redsocks dnsutils
```

HTTPS returned 200 after CONNECT 200. LAN CONNECT returned 403. Apt fetched indexes and installed packages using plain HTTP forwarding. The [spike proxy](../../spikes/phase0/proxy.js) is IPv4-only and dials the checked address directly. Production-grade route validation is UNVERIFIED.

## E. Transparent TCP and Docker

With the DNS relay active, streamed [the guest script](../../spikes/phase0/guest-proxy.sh) using `limactl shell spike-homebox -- sudo sh -s -- transparent < spikes/phase0/guest-proxy.sh`. It set up redsocks and redirected guest TCP OUTPUT for ports 22, 80 and 443 to local port 12345. With proxy environment variables absent:

```sh
curl -4 --noproxy '*' -fsS https://example.com -o /dev/null
git clone --depth=1 https://github.com/octocat/Hello-World.git "$(mktemp -d)"
ssh -4 -o BatchMode=yes -o StrictHostKeyChecking=accept-new -T git@github.com
```

Curl returned 200 and the clone succeeded. GitHub SSH completed host-key exchange and returned `Permission denied (publickey)`. Authenticated GitHub SSH is UNVERIFIED.

Installed `docker.io` via apt using the proxy. A Docker systemd drop-in set `HTTP_PROXY` and `HTTPS_PROXY` to `http://192.168.5.2:23128`. After `systemctl daemon-reload` and `systemctl restart docker`, `docker run --rm` pulled Alpine. BusyBox wget with proxy environment succeeded for HTTP and received 403 for HTTPS. Bridge HTTPS worked with a DNS pin:

```sh
sudo sed -i 's/local_ip = 127.0.0.1/local_ip = 0.0.0.0/' /etc/redsocks.conf
sudo systemctl restart redsocks
sudo iptables -t nat -I PREROUTING -i docker0 -p tcp --dport 443 -j REDIRECT --to-ports 12345
target=$(getent ahostsv4 example.com | awk 'NR==1 {print $1}')
sudo docker run --rm --add-host "example.com:$target" alpine wget -qO /dev/null https://example.com
```

## F. Inbound publishing

`ssh.localPort: 2201` listened on `127.0.0.1:2201`. Lima rejected a static forward to guest port 22 with `field portForwards[0].guestPort must not be 22`. The [SSH relay](../../spikes/phase0/ssh-relay.js) bound `0.0.0.0:2202` and forwarded to Lima loopback SSH.

A plain-mode single-port rule worked with `(local tcp "*:20200")` in Seatbelt:

```yaml
portForwards:
- guestPort: 8000
  hostIP: 0.0.0.0
  hostPort: 20200
  static: true
```

The host saw HTTP 200 with two individual LAN rules and HTTP 200 with the checked-in loopback rule. A static `guestPortRange: [8000, 8099]` to `hostPortRange: [20100, 20199]` warned of unconditional forwarding and created zero listeners. Creating all 100 as individual rules is UNVERIFIED. Fresh `plain: true` preserved SSH and static forwards, left `lima-guestagent` inactive and did not publish an unconfigured guest HTTP listener.

## G. Extra SSH key

```sh
ssh-keygen -q -t ed25519 -N '' -C phase0 -f "$HOME/.homebox-spike/ssh-probe"
limactl shell spike-homebox -- sh -c 'umask 077; cat >> ~/.ssh/authorized_keys' \
  < "$HOME/.homebox-spike/ssh-probe.pub"
# With node spikes/phase0/ssh-relay.js running separately
ssh -F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="$HOME/.homebox-spike/known_hosts" \
  -i "$HOME/.homebox-spike/ssh-probe" -p 2202 homebox@<host-lan-ip> id -un
```

SSH from the same Mac via its LAN address returned `homebox`. A separate physical LAN client is UNVERIFIED.

## H. Other isolation knobs

`findmnt -rn -t virtiofs,9p,fuse.sshfs` returned no entries. `SSH_AUTH_SOCK` was absent, containerd was inactive before Docker was installed, and proxy environment variables were absent. `id -un` and `$HOME` matched `homebox` and `/home/homebox`. The first non-plain run's ignore rule matched too narrowly and Lima attempted automatic forwards. Lima documents using `guestIP: 0.0.0.0`, `guestIPMustBeZero: false`, `proto: any` and `ignore: true` together. The corrected non-plain rule is UNVERIFIED here. [Lima port forwarding documentation](https://lima-vm.io/docs/config/port/)

Lima still advertised a host search suffix with the host resolver disabled and `dns: [127.0.0.1]`. The runtime `resolvectl domain eth0 '~.'` command in C replaces the link's search list with a routing-only entry. Durable suppression of inherited search domains is UNVERIFIED.

## I. Timing and cleanup

Cold download, conversion and creation took 35 seconds. The first sandboxed boot took 11 seconds, for 46 seconds of total command time. Cached create took 0.35 seconds. The fresh final plain-mode start reached SSH after about 10 seconds and readiness after 42 seconds, with about 33 seconds waiting for boot scripts. The cause of that wait is UNVERIFIED. A sandboxed restart took about 11 seconds. Deleted all three spike VMs with `limactl delete -f` and stopped the temporary host listeners.

## Recommended design for box on macOS

1. Generate a per-VM Lima config with the image-only base, VZ user-mode networking, plain mode, no mounts, no agent forwarding, no host resolver and an explicit guest user. Suppress inherited DNS search domains in the guest network config. Create before entering Seatbelt so Lima can download and verify the image.
2. Start a loopback HTTP/CONNECT proxy per VM. Validate and dial the resolved address. Include checks for host interface addresses and local routes alongside special-use ranges. Reserve its port for the lifetime of the sandboxed hostagent.
3. Render the instance-local Seatbelt profile with exact listener and outbound ports. Start and restart through it. Track the hostagent box started so doctor can detect an instance started outside the profile.
4. Configure proxy-aware clients first. For transparent TCP, add controlled DNS and guest redirects. Keep enforcement on the host when guest root removes its redirects.
5. Use individual static HTTP rules. Bind on loopback by default and on the LAN only when opted in. Use a box TCP relay for LAN SSH on a separate port. Verify actual listeners and public-key authentication.
6. Have doctor test direct denials and proxy success with known-live controls. Repeat LAN SSH from the MacBook before closing Phase 0. Hostagent compromise and filesystem confinement are UNVERIFIED.
