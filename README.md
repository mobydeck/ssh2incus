# ssh2incus – SSH server for Incus instances

**ssh2incus** is an git remote add origin https://github.com/mobydeck/ssh2incus.git.
It uses Incus API in order to establish a connection with an instance and create a session.

## Features

- Authentication using existing host OS SSH keys via `authorized_keys`
- SSH Agent forwarding into an instance session
- Full support for PTY (terminal) mode and remote command execution
- Full support for SCP and SFTP (SFTP server is embedded into `ssh2incus`)
- Full Ansible support
- Local port forwarding support
- Graceful termination via OS signals (SIGINT, SIGTERM)

## Enterprise Features

- Authentication using any possible method (keys, passwords, external API integration, LDAP etc)
- Web browser based access to an instance shell using JWT tokens
- 24/7 technical support and new feature development

## Installation

Download the latest package from **Releases** to an Incus host and install 

#### On Ubuntu / Debian

```
apt-get install -f ./ssh2incus_0.1.0-0_amd64.deb
```

#### On RHEL / Fedora / CentOS / AlmaLinux / Rocky Linux

```
yum install ./ssh2incus-0.1.0-0.x86_64.rpm
```

#### Enable and start ssh2incus service

```
systemctl enable ssh2incus.service
systemctl start ssh2incus.service
```

#### Checking logs

```
journalctl -f -u ssh2incus.service
```

## Basic Connection

To establish an SSH connection to an instance running on Incus host, run:

```
ssh -p 2222 [instance-user@]instance-name[.project-name][+host-user]@incus-host
```

and substitute the following

- `host-user` – active user on Incus host such as `root`
- `instance-name` – active instance on Incus host
- `project-name` – Incus project the instance is running under
- `instance-user` – active user in Incus instance (_optional, defaults to_ `root`)
- `incus-host` – Incus host hostname or IP

### Examples

To connect to an instance `ubuntu` running on Incus host with IP `1.2.3.4` as `root` user and authenticate
as `root` on Incus host, run:

```
ssh -p 2222 ubuntu@1.2.3.4
```

To connect to an instance `ubuntu` running on Incus host with IP `1.2.3.4` as `root` user and authenticate
as `admin` on Incus host, run:

```
ssh -p 2222 ubuntu+admin@1.2.3.4
```

To connect to an instance `ubuntu` running on Incus host with IP `1.2.3.4` as `ubuntu` user and authenticate
as `root` on Incus host, run:

```
ssh -p 2222 ubuntu@ubuntu+root@1.2.3.4
```

To connect to an instance `ubuntu` under `project1` project running on Incus host with IP `1.2.3.4` as `ubuntu`
user and authenticate as `root` on Incus host, run:

```
ssh -p 2222 ubuntu@ubuntu.project1+root@1.2.3.4
```

## Advanced Connection

### SSH Agent forwarding

`ssh2incus` supports SSH Agent forwarding. To make it work in an instance, it will automatically add a
proxy socket device to Incus instance and remove it once SSH connection is closed.

To enable SSH agent on your local system, run:

```
eval `ssh-agent`
```

To enable SSH Agent forwarding when connecting to an instance add `-A` to your `ssh` command

```
ssh -A -p 2222 ubuntu@1.2.3.4
```

### Local port forwarding

Local port forwarding allows forwarding connections on a local port to a port on Incus instance.

To forward local port `8080` listening on `127.0.0.1` to port `80` on `ubuntu` instance, run:

```
ssh -L 127.0.0.1:8080::80 -p 2222 ubuntu@1.2.3.4
```

`ssh2incus` will automatically resolve the IP address of an instance to create
port forwarding tunnel.

### Using Incus host as SSH Proxy / Bastion

You can access an Incus instance by using Incus host's SSH server as a Proxy / Bastion.
The easiest way is to add additional configuration to your `~/.ssh/config`

```
Host incus1
  Hostname localhost
  Port 2222
  ProxyJump incus-host

Host incus-host
  Hostname 1.2.3.4
  User root
```

Now to connect to `ubuntu` instance as `root`, run:

```
ssh ubuntu@incus1
```

> Using this method has additional security benefits and port 2222 is not exposed to the public

## Server Management

### Graceful Termination

The server has built-in support for graceful termination via OS signals. When receiving SIGINT (Ctrl+C) or SIGTERM signals, the server will:

1. Stop accepting new connections
2. Allow existing connections to complete their work (up to a 5-second timeout)
3. Shut down cleanly

### Ansible

#### Examples

```
ansible.cfg:

[defaults]
host_key_checking = False
remote_tmp = /tmp/.ansible-${USER}
```

```
inventory:

# Direct connection to port 2222
[incus1]
instance-a ansible_user=root+c1 ansible_host=1.2.3.4 ansible_port=2222
instance-b ansible_user=root+u1+ubuntu ansible_host=1.2.3.4 ansible_port=2222 become=yes

# Connection using ProxyJump configured in ssh config 
[incus2]
instance-c ansible_user=root+c1 ansible_host=incus1
instance-d ansible_user=root+u1+ubuntu ansible_host=incus1 become=yes
```

```
playbook.yml:

---
- hosts: incus1,incus2
  become: no
  become_method: sudo

  tasks:
    - command: env
    - command: ip addr
```


## Configuration Options

By default `ssh2incus` will listen on port `2222` and allow authentication for `root` and users who belong to the groups
`adm,incus` on Ubuntu / Debian Incus host and `wheel,incus` on RHEL Incus host.

To add a user to one of those groups run as root `usermod -aG incus your-host-user`

To run `ssh2incus` with custom configuration options you can edit `/etc/default/ssh2incus`.
The following options can be added to `ARGS=`

```
  -c, --client-cert string   client certificate for remote
  -k, --client-key string    client key for remote
  -d, --debug                enable debug log
  -g, --groups string        list of groups members of which allowed to connect (default "incus")
      --healthcheck string   enable Incus health check every X minutes, e.g. "5m"
  -h, --help                 print help
  -l, --listen string        listen on :2222 or 127.0.0.1:2222 (default ":2222")
      --noauth               disable SSH authentication completely
  -r, --remote string        Incus remote defined in config.yml, e.g. my-remote
  -t, --server-cert string   server certificate for remote
      --shell string         shell access command: login, su or default shell
  -s, --socket string        Incus socket or use INCUS_SOCKET
  -u, --url string           Incus remote url starting with https://
  -v, --version              print version
```

For example, to enable debug log and listen on localhost change the line to `ARGS=-d -l 127.0.0.1:2222`

### Firewall

If you have firewall enabled on your Incus host, you may need to allow connections to port `2222`

On Ubuntu / Debian

```
ufw allow 2222/tcp
ufw reload
```

On RHEL / CentOS / AlmaLinux

```
firewall-cmd --permanent --add-port=2222/tcp
firewall-cmd --reload
```

## Support

Community support is available through **GitHub Issues**.
