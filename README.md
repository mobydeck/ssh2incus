# ssh2incus – SSH server for Incus instances

_β (beta)_

`ssh2incus` provides a full-featured SSH server that connects directly to
[Incus](https://linuxcontainers.org/incus/) containers and virtual machines. It runs on the Incus host
and intelligently routes incoming SSH connections to the appropriate instances using the Incus API, eliminating the
need to run SSH servers inside the instances.

## Features

### Core Features

- **Authentication**: Uses existing host ssh keys by default or, additionally, ssh keys inside instances (`--inauth`) 
- **No-Auth Mode**: Optional authentication-free mode for local and development environments (`--noauth`)
- **Multiple Remotes**: Connect to any remote from `incus remote list`
- **Terminal Support**: Full PTY (terminal) mode and remote command execution
- **File Transfer**: Complete SCP and SFTP support with integrated SFTP server
- **Port Forwarding**:
    - Local forwarding (`ssh -L`)
    - Reverse forwarding (`ssh -R`)
    - Dynamic forwarding (`ssh -D`)

- **SSH Agent Forwarding**: Seamlessly forward your SSH agent into instance sessions
- **Process Models**:
    - Master process mode: Maintains SSH connections after service restart
    - Daemon mode: Single process with multiple threads for resource-constrained systems

- **Incus Shell**: Manage Incus over SSH

- **Compatibility**:
    - Built using Incus 6.x API
    - Works with Incus inside Lima and Colima
    - Tested with **Jetbrains Gateway**, **VSCode**, **Cursor** and other IDEs
    - Full Ansible support

### Enterprise Features

- Advanced authentication options (SSH keys, passwords, JWT, OpenID, OAuth 2.0, LDAP, etc.)
- Web browser-based terminal access to instance shells
- 24/7 technical support with prioritized feature development

## Installation

Download the latest package from the [Releases](https://github.com/mobydeck/ssh2incus/releases) page and install:

### Debian-based Systems (Ubuntu, Debian)

```shell
apt-get install -f ./ssh2incus_0.6-0_amd64.deb
```

### RPM-based Systems (RHEL, Fedora, CentOS, AlmaLinux, Rocky Linux)

```shell
yum install ./ssh2incus-0.6-0.x86_64.rpm
```

### Service Management

Start and enable the service:

```shell
systemctl enable ssh2incus.service
systemctl start ssh2incus.service
```

Monitor logs:

```shell
journalctl -f -u ssh2incus.service
```

## Connection Guide

### Connection Format

To establish an SSH connection to an instance running on Incus host, run:

```shell
ssh -p 2222 [remote:][instance-user@]instance-name[.project-name][+host-user]@incus-host
```

Where:

- `instance-name`: Name of a running instance (required)
- `remote`: Remote name from `incus remote list` (optional, defaults to either current remote or remote set via `-r` flag)
- `instance-user`: User in the Incus instance (optional, defaults to `root`)
- `project-name`: Incus project name (optional, defaults to `default`)
- `host-user`: User on the Incus host (optional, defaults to `root`)
- `incus-host`: Hostname or IP address of the Incus host where `ssh2incus` is running (required)

### Connection Examples

#### Basic Connection

Connect to instance `ubuntu` as `root`:

```shell
ssh -p 2222 ubuntu@1.2.3.4
```

#### Specify Host User

Connect to instance `ubuntu` as root using `admin` on the host:

```shell
ssh -p 2222 ubuntu+admin@1.2.3.4
```

#### Specify Instance User

Connect to instance `ubuntu` as user `ubuntu` using host user `admin`:

```shell
ssh -p 2222 ubuntu@ubuntu+admin@1.2.3.4
```

#### Specify Project

Connect to instance `ubuntu` in `project1` as user `ubuntu`:

```shell
ssh -p 2222 ubuntu@ubuntu.project1@1.2.3.4
```

#### Specify Remote

Connect to instance `ubuntu` in `project1` on remote `incus-prod` as user `ubuntu`:

```shell
ssh -p 2222 incus-prod:ubuntu@ubuntu.project1@1.2.3.4
```

## Advanced Features

### SSH Agent Forwarding

Enable SSH agent forwarding to use your local SSH keys inside the instance:

1. Start SSH agent locally:
    ```shell
    eval `ssh-agent`
    ```
2. Connect with agent forwarding:
    ```shell
    ssh -A -p 2222 ubuntu@1.2.3.4
    ```

`ssh2incus` automatically creates a proxy socket device in the instance and removes it when the connection closes.

### Port Forwarding

#### Local Port Forwarding

Forward local port 8080 to port 80 on the instance:

To forward local port `8080` listening on `127.0.0.1` to port `80` on `ubuntu` instance, run:

```shell
ssh -L 127.0.0.1:8080::80 -p 2222 ubuntu@1.2.3.4
```

#### Reverse Port Forwarding

Forward remote port 3000 on the instance to local port 8080 on your machine:

```shell
ssh -R 127.0.0.1:3000:127.0.0.1:8080 -p 2222 ubuntu@1.2.3.4
```

#### Dynamic Port Forwarding

Dynamic port forwarding sets up a SOCKS5 proxy server through your SSH connection, allowing applications to route
traffic through it regardless of destination port:

```shell
ssh -D 1080 -p 2222 ubuntu@1.2.3.4
```

### Using Incus Host as SSH Proxy / Bastion

You can access Incus instances through the Incus host acting as an SSH proxy or bastion.
Configure this in your `~/.ssh/config`:

```
Host incus1
  Hostname localhost
  Port 2222
  ProxyJump incus-host

Host incus-host
  Hostname 1.2.3.4
  User root
```

Now connect to the `ubuntu` instance as `root` with:

```shell
ssh ubuntu@incus1
```

> **Security Note**: Using this method provides additional security benefits since port 2222 is
> not exposed to the public internet.

## Server Management

### Process Modes

`ssh2incus` offers two operating modes to suit different environments and requirements:

#### Master Process Mode (Default)

Master process mode employs a primary process that spawns child processes for handling connections. This architecture provides:
- **High Availability**: SSH connections remain active even if the `ssh2incus` service is restarted
- **Fault Isolation**: Issues with one connection don't affect others
- **Resource Management**: Better utilization of system resources for handling many concurrent connections
- **Seamless Updates**: Update the `ssh2incus` service without disrupting active sessions

This is the recommended mode for production environments or systems with sufficient resources.

#### Daemon Mode

Daemon mode runs as a single process with multiple threads, designed for:
- **Low Memory Usage**: Significantly reduced memory footprint
- **Simple Architecture**: Single-process design for embedded or resource-constrained systems
- **Lighter Load**: Better performance on systems with very limited resources

To enable daemon mode, modify `/etc/default/ssh2incus`: remove `-m` from `ARGS=`.

> **Note**: In daemon mode, all active connections will be terminated if the `ssh2incus` service is restarted.

### Incus Shell

The `%shell` command provides direct access to the Incus command line interface from an SSH session,
allowing you to manage your Incus instances without needing to log into the host directly.

Only root is allowed to connect to Incus shell.

This feature is especially useful for:
- Quick management tasks without direct host access
- Systems administration from remote locations

#### Usage

To access the Incus shell, connect using:

```shell
ssh -p 2222 %shell@incus-host
```

#### Features

- Interactive Incus command execution
- Ctrl+C to exit the shell cleanly

#### Example Session

```shell
$ ssh %shell@incus-colima

incus shell emulator on colima-incus (Ctrl+C to exit)

Hit Enter or type 'help <command>' for help about any command

Type incus command:
> incus version
Client version: 6.11
Server version: 6.11

Type incus command:
> incus
```

## Ansible

`ssh2incus` fully supports Ansible automation for container management, making it simple to orchestrate your
Incus instance configuration.

### Ansible Configuration

Create an `ansible.cfg` file with optimized settings for Incus instances:

```
[defaults]
host_key_checking = False
remote_tmp = /tmp/.ansible-${USER}
```

### Inventory Examples

#### Direct Connection

Connect directly to `ssh2incus` on port 2222:

```
[incus1]
# Basic instance connection
instance-a ansible_user=c1 ansible_host=1.2.3.4 ansible_port=2222

# Connection with privilege escalation
instance-b ansible_user=u1@ubuntu ansible_host=1.2.3.4 ansible_port=2222 become=yes
```

#### ProxyJump Connection

Use SSH configuration for a cleaner approach (requires ProxyJump configuration in `~/.ssh/config`):

```
[incus2]
# Basic instance connection
instance-c ansible_user=c1 ansible_host=incus1

# Connection with privilege escalation
instance-d ansible_user=u1@ubuntu ansible_host=incus1 become=yes
```

### Example Playbook

```yaml
---
- hosts: incus1,incus2
  become: no
  become_method: sudo

  tasks:
    - command: env
    - command: ip addr
```

## Configuration

### Default Behavior

By default, `ssh2incus`:

- Listens on port `2222`
- Permits authentication for `root` and members of the `incus`, `incus-admin` groups

To grant a user access permission:

```shell
sudo usermod -aG incus your-host-user
```

### Custom Configuration

Modify `ssh2incus` behavior by editing `/etc/default/ssh2incus`. Add configuration flags to the `ARGS=` line.

### Available Options

```
  -b, --banner                show banner on login
  -c, --client-cert string    client certificate for remote
  -k, --client-key string     client key for remote
  -d, --debug                 enable debug log
  -g, --groups string         list of groups members of which allowed to connect (default "incus,incus-admin")
      --healthcheck string    enable Incus health check every X minutes, e.g. "5m"
  -h, --help                  print help
      --inauth                enable authentication using instance keys
  -l, --listen string         listen on ":port" or "host:port" (default ":2222")
  -m, --master                start master process and spawn workers
      --noauth                disable SSH authentication completely
      --pprof                 enable pprof
      --pprof-listen string   pprof listen on ":port" or "host:port" (default ":6060")
  -r, --remote string         default Incus remote to use
  -t, --server-cert string    server certificate for remote
      --shell string          shell access command: login, su, sush or user shell
  -s, --socket string         Incus socket to connect to (optional, defaults to INCUS_SOCKET env)
  -u, --url string            Incus remote url to connect to (should start with https://)
  -v, --version               print version
  -w, --welcome               show welcome message to users connecting to shell
```

#### Configuration Example

To enable debug logging and restrict listening to localhost:

```
ARGS=-d -l 127.0.0.1:2222
```

### Banner

Enable the optional welcome banner with the `-b` flag:

```
┌──────────────────────────────────────────────┐
│          _     ____  _                       │
│  ___ ___| |__ |___ \(_)_ __   ___ _   _ ___  │
│ / __/ __| '_ \  __) | | '_ \ / __| | | / __| │
│ \__ \__ \ | | |/ __/| | | | | (__| |_| \__ \ │
│ |___/___/_| |_|_____|_|_| |_|\___|\__,_|___/ │
└──────────────────────────────────────────────┘
👤 root 📦 a9.default 💻 colima-incus
────────────────────────────────────────────────
```

The banner provides useful context showing:
- Current user (👤)
- Container/VM name and project (📦)
- Remote / host system name (💻)

## Firewall Configuration

Since `ssh2incus` listens on port `2222` by default, you'll need to configure your firewall to allow incoming connections on this port.

### Ubuntu/Debian (UFW)

```shell
# Allow SSH access on port 2222
sudo ufw allow 2222/tcp

# Apply changes
sudo ufw reload

# Verify the rule was added
sudo ufw status
```

### RHEL / CentOS / AlmaLinux / Rocky Linux (firewalld)

```shell
# Allow SSH access on port 2222
sudo firewall-cmd --permanent --add-port=2222/tcp

# Apply changes
sudo firewall-cmd --reload

# Verify the rule was added
sudo firewall-cmd --list-ports
```

### Other Firewall Systems

#### iptables (direct)

```shell
sudo iptables -A INPUT -p tcp --dport 2222 -j ACCEPT
sudo iptables-save > /etc/iptables/rules.v4  # Make changes persistent
```

#### nftables

```shell
sudo nft add rule inet filter input tcp dport 2222 accept
sudo nft list ruleset > /etc/nftables.conf  # Make changes persistent
```

### Security Recommendations

For production environments, consider these firewall best practices:
1. **Restrict Access**: Limit access to port 2222 to specific IP addresses or networks
2. **Use SSH Bastion**: Consider exposing `ssh2incus` only to your management network
3. **Rate Limiting**: Implement connection rate limiting to prevent brute force attacks

## Support Options

### Community Support

Get help from the community and developers:
- **GitHub Issues**: Report bugs, request features, or ask questions through the [GitHub repository](https://github.com/mobydeck/ssh2incus/issues)
- **Documentation**: Refer to the [online documentation](https://ssh2incus.com/documentation) for detailed guides and tutorials

### Enterprise Support

For organizations requiring guaranteed response times and dedicated assistance:
- **Technical Support**: 24/7 access to technical support engineers
- **Priority Bug Fixes**: Expedited resolution of critical issues
- **Feature Development**: Priority implementation of custom requirements
- **Consulting Services**: Implementation guidance and best practices

### Troubleshooting Tips

If you encounter issues with `ssh2incus`:
1. **Check logs**: `journalctl -u ssh2incus.service`
2. **Verify service status**: `systemctl status ssh2incus.service`
3. **Test connectivity**: `telnet incus-host 2222`
4. **Enable debug mode**: Set `-d` flag in `/etc/default/ssh2incus`
5. **Check permissions**: Ensure your host user belongs to the proper groups (`incus` or `incus-admin`)
