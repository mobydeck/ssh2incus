# Instance Creation Configuration Guide

This guide provides comprehensive documentation for ssh2incus's advanced instance creation and configuration system, including profile-based configuration, file includes, and dynamic instance creation workflows.

## Table of Contents

1. [Quick Start - Example Web Server](#quick-start---example-web-server)
2. [Overview](#overview)
3. [Configuration File Structure](#configuration-file-structure)
4. [Profile-Based Configuration](#profile-based-configuration)
5. [File Include Functionality](#file-include-functionality)
6. [Instance Creation Syntax](#instance-creation-syntax)
7. [Configuration Precedence](#configuration-precedence)
8. [Cloud-Init Configuration](#cloud-init-configuration)
9. [Practical Examples](#practical-examples)
10. [Advanced Use Cases](#advanced-use-cases)
11. [Troubleshooting](#troubleshooting)

## Quick Start - Example Web Server

ssh2incus includes a ready-to-use web server example that demonstrates dynamic instance creation with cloud-init configuration. This example automatically deploys a fully-configured nginx web server with a custom demo website.

### Using the Example

The example configuration is included in the default `create-config.yaml`:

```yaml
profiles:
    web-server-example:
        image: ubuntu/22.04/cloud
        memory: 2
        cpu: 2
        disk: 20
        config:
            cloud-init.user-data: <@web-server-init.example.yaml
```

**Files involved:**
- **Configuration profile**: `packaging/create-config.yaml` - Contains the `web-server-example` profile
- **Cloud-init setup**: `packaging/web-server-init.example.yaml` - Full web server deployment script

### What Gets Deployed

The example automatically:
- ✓ Installs and configures nginx web server
- ✓ Deploys a custom demo website with HTML, CSS, and JavaScript
- ✓ Creates a sample API endpoint (`/api/status`)
- ✓ Configures firewall rules (ports 80, 443)
- ✓ Sets up security headers and caching
- ✓ Provides zero-touch deployment ready in seconds

### Try It

Create a web server instance using the example profile:

```bash
ssh user+web-server-example@incus-host
```

Once deployed, access the website at `http://YOUR_SERVER_IP/` and test the API with:

```bash
curl http://YOUR_SERVER_IP/api/status
```

### Customize the Example

**DO NOT edit the example files directly** - they will be overwritten on updates. Instead:

1. Copy the example file:
   ```bash
   cp /path/to/web-server-init.example.yaml my-web-server-init.yaml
   ```

2. Create your own profile in `create-config.yaml`:
   ```yaml
   profiles:
       my-web-server:
           image: ubuntu/22.04/cloud
           memory: 2
           cpu: 2
           config:
               cloud-init.user-data: <@my-web-server-init.yaml
   ```

3. Modify `my-web-server-init.yaml` with your custom configuration

This example demonstrates the power of ssh2incus's file include functionality (`<@` syntax) and cloud-init integration for automated instance configuration. See the sections below for more details on these features.

## Overview

ssh2incus provides a powerful configuration system for creating instances with customizable settings. The system supports:

- **Profile-based configuration**: Reusable configuration templates
- **File includes**: External file content injection
- **Hierarchical configuration**: Override precedence system
- **Dynamic instance creation**: On-demand instance provisioning via SSH login strings

## Configuration File Structure

The main configuration file (`create-config.yaml`) defines default settings and reusable profiles for instance creation.

### Basic Structure

```yaml
defaults:
    # Default settings for all instances
    image: alpine/edge
    ephemeral: false
    memory: 2
    cpu: 2
    disk: 10
    vm: false
    devices: {}
    config:
        security.privileged: false
        security.nesting: false
        security.syscalls.intercept.mknod: false
        security.syscalls.intercept.setxattr: false

profiles:
    # Named configuration profiles
    profile-name:
        # Profile-specific overrides
```

### Configuration Options

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `image` | string | Container/VM image | `alpine/edge` |
| `ephemeral` | boolean | Create ephemeral instance | `false` |
| `memory` | integer | Memory limit in GiB (0 = unlimited) | `2` |
| `cpu` | integer | CPU limit (0 = unlimited) | `2` |
| `disk` | integer | Root disk size in GiB (0 = unlimited) | `10` |
| `vm` | boolean | Create VM instead of container | `false` |
| `devices` | object | Instance devices configuration | `{}` |
| `config` | object | Instance configuration key-value pairs | `{}` |

## Profile-Based Configuration

Profiles are named configuration templates that can be applied during instance creation. They allow you to define reusable sets of configuration options for different use cases.

### Defining Profiles

```yaml
profiles:
    web-server:
        image: ubuntu/24.04/cloud
        memory: 4
        cpu: 2
        disk: 20
        config:
            security.nesting: true
            user.user-data: |
                #cloud-config
                packages:
                  - nginx
                  - certbot
                runcmd:
                  - systemctl enable nginx
                  - systemctl start nginx

    database:
        image: ubuntu/24.04/cloud
        memory: 8
        cpu: 4
        disk: 50
        config:
            security.privileged: true
            user.user-data: |
                #cloud-config
                packages:
                  - postgresql
                  - postgresql-contrib
                runcmd:
                  - systemctl enable postgresql
                  - systemctl start postgresql

    development:
        image: ubuntu/24.04/cloud
        memory: 4
        cpu: 2
        disk: 30
        config:
            security.nesting: true
            security.privileged: true
            user.user-data: |
                #cloud-config
                packages:
                  - build-essential
                  - git
                  - docker.io
                users:
                  - name: developer
                    groups: docker
                    sudo: ALL=(ALL) NOPASSWD:ALL
```

### Using Profiles

> **💡 Tip**: ssh2incus includes a ready-to-use `web-server-example` profile that demonstrates nginx deployment with cloud-init. See the [Quick Start section](#quick-start---example-web-server) above for details.

Profiles are applied using the `%profile-name` syntax in SSH login strings:

```bash
# Apply single profile
ssh +myinstance+%web-server@host

# Apply multiple profiles (later profiles override earlier ones)
ssh +myinstance+%web-server+%development@host

# Combine profiles with direct configuration
ssh +myinstance+%web-server+m8+c4@host  # 8GB RAM, 4 CPUs
```

## File Include Functionality

File includes allow you to inject external file content into configuration values, enabling better organization and reusability of configuration data.

> **💡 Example**: The included `web-server-example` profile demonstrates file includes with `<@web-server-init.example.yaml`. See the [Quick Start section](#quick-start---example-web-server) for a complete working example.

### Include Syntax

Two syntaxes are supported for file includes:

1. `!include filename.ext` - Standard include directive
2. `<@filename.ext` - Alternative include syntax

### Path Resolution

Include files are resolved using the following priority:

1. **Relative to config file**: If the include path is relative, first try relative to the directory containing the configuration file
2. **Current working directory**: Fall back to the current working directory
3. **Absolute paths**: Used as-is

### Include Examples

> **💡 Real Example**: ssh2incus includes a `web-server-example` profile in `packaging/create-config.yaml` that uses `<@web-server-init.example.yaml` to deploy a complete nginx web server. See the [Quick Start section](#quick-start---example-web-server).

```yaml
profiles:
    cloud-init-example:
        image: ubuntu/24.04
        config:
            # Include cloud-init configuration from external file
            user.user-data: !include cloud-init.yaml

    web-server:
        image: ubuntu/24.04
        config:
            # Alternative include syntax
            user.user-data: <@web-server-init.yaml
            # Include shell scripts
            user.vendor-data: <@setup-script.sh

    database:
        image: ubuntu/24.04
        config:
            # Multiple includes in same profile
            user.user-data: !include db-cloud-init.yaml
            user.meta-data: <@db-metadata.yaml
```

### Example Include Files

**cloud-init.yaml:**
```yaml
#cloud-config
package_update: true
package_upgrade: true
packages:
    - git
    - curl
    - wget
users:
    - name: admin
      passwd: $6$rounds=4096$salt$hash
      lock_passwd: false
      sudo: ALL=(ALL) NOPASSWD:ALL
      ssh_authorized_keys:
          - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBMuZwNxXpAyxLXJYtAsSK6cVWp0zTSw+6JulOXn2O9
runcmd:
    - touch /tmp/initialization-complete
```

**setup-script.sh:**
```bash
#!/bin/bash
apt-get update
apt-get install -y nginx
systemctl enable nginx
systemctl start nginx
echo "Web server setup complete" > /var/log/setup.log
```

## Instance Creation Syntax

### Creation Modes

ssh2incus supports two instance creation modes:

- **Persistent instances** (`+` prefix): Remain after SSH session ends
- **Ephemeral instances** (`~` prefix): Automatically deleted when session ends

### Login String Format

```
[~|+][remote:]instance[.project][+configuration]@host
```

### Configuration String Components

Configuration strings use `+` as a separator and support:

| Component | Syntax | Example | Description |
|-----------|--------|---------|-------------|
| Profile | `%profile-name` | `%web-server` | Apply named profile |
| Multiple Profiles | `%profile1+%profile2` | `%base+%web-server` | Apply profiles in order |
| Image | `image/name` | `ubuntu/24.04` | Specify container image |
| Memory | `m<number>` | `m4` | Memory in GiB |
| CPU | `c<number>` | `c2` | Number of CPUs |
| Disk | `d<number>` | `d20` | Disk size in GiB |
| Nesting | `n` or `nest` | `n` | Enable container nesting |
| Privileged | `p` or `priv` | `p` | Enable privileged mode |
| Ephemeral | `e` or `ephe` | `e` | Make instance ephemeral |

### Basic Examples

```bash
# Create persistent instance with defaults
ssh +myinstance@host

# Create ephemeral instance
ssh ~myinstance@host

# Create with specific image
ssh +myinstance+ubuntu/22.04@host

# Create with cloud-init enabled image
ssh +myinstance+ubuntu/24.04/cloud@host

# Create with resource limits
ssh +myinstance+m4+c2+d20@host

# Create with profile
ssh +myinstance+%web-server@host
```

## Configuration Precedence

Configuration values are resolved using the following precedence order (highest to lowest):

1. **Direct SSH login options** - Explicit values in the login string
2. **Profiles** - Applied in the order specified (later profiles override earlier ones)
3. **Defaults** - Base configuration from `create-config.yaml`

### Precedence Example

Given this configuration:

```yaml
defaults:
    image: alpine/edge
    memory: 2
    cpu: 1

profiles:
    web-server:
        image: ubuntu/24.04
        memory: 4

    performance:
        memory: 8
        cpu: 4
```

And this SSH command:
```bash
ssh +myapp+%web-server+%performance+c2@host
```

Final configuration will be:
- `image`: `ubuntu/24.04` (from web-server profile)
- `memory`: `8` (from performance profile, overriding web-server)
- `cpu`: `2` (from direct login option, overriding performance profile)

## Cloud-Init Configuration

> **💡 Complete Example**: ssh2incus includes a full-featured `web-server-example` profile in `packaging/create-config.yaml` with `packaging/web-server-init.example.yaml` demonstrating cloud-init configuration for nginx web server deployment. See the [Quick Start section](#quick-start---example-web-server) for details.

Incus supports cloud-init configuration through two different sets of configuration options, depending on the image version you're using:

- **Modern images**: Use `cloud-init.*` configuration options
- **Older images**: Use `user.*` configuration options

As a rule of thumb, newer images support the `cloud-init.*` configuration options, while older images support `user.*`. However, there might be exceptions to this rule.

### Cloud-Init Image Requirements

**Important**: To use cloud-init configuration options, you must use images with the `/cloud` suffix:

- `ubuntu/24.04/cloud` (instead of `ubuntu/24.04`)
- `alpine/edge/cloud` (instead of `alpine/edge`)
- `debian/12/cloud` (instead of `debian/12`)

Regular images without the `/cloud` suffix do not include cloud-init and will ignore these configuration options.

### Supported Configuration Options

The following configuration options are supported for cloud-init:

| Option | Description |
|--------|-------------|
| `cloud-init.vendor-data` or `user.vendor-data` | General default configuration data |
| `cloud-init.user-data` or `user.user-data` | Instance-specific configuration data |
| `cloud-init.network-config` or `user.network-config` | Network configuration data |

### Vendor Data vs User Data

Both vendor-data and user-data are used to provide cloud configuration data to cloud-init:

- **Vendor-data**: Used for general default configuration that should be applied to multiple instances
- **User-data**: Used for instance-specific configuration unique to each instance

**Best Practice**: Specify vendor-data in profiles and user-data in instance configuration. While Incus allows using both in profiles and instance configurations, following this convention improves maintainability.

### Configuration Merging

When both vendor-data and user-data are supplied for an instance, cloud-init merges the two configurations. However, if you use the same keys in both configurations, merging might not be possible. In such cases, you need to configure how cloud-init should merge the provided data.

### Cloud-Init Examples Reference

For comprehensive cloud-init configuration examples, refer to the official cloud-init documentation:

#### System initialization and boot
- [Run commands during boot](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/boot_cmds.html#cce-boot-cmds) using `bootcmd` and `runcmd`
- [Control vendor-data use](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/scripts.html#cce-scripts) on boot
- [Provide random seed data](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/seed_random.html#cce-seed-random)
- [Writing out arbitrary files](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/write_files.html#cce-write-files)

#### System configuration
- [Set keyboard layout](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/keyboard.html#cce-keyboard)
- [Set system locale and timezone](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/locale_and_timezone.html#cce-locale-timezone)

#### Manage users
- [Configure users and groups](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/user_groups.html#cce-user-groups)
- [User passwords](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/set_passwords.html#cce-set-passwords)

#### Package management
- [Update, upgrade and install packages](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/package_update_upgrade.html#cce-update-upgrade)
- [Configure APT](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/apt.html#cce-apt)
- [APT pipelining](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/apt_pipeline.html#cce-apt-pipeline)
- [Configure APK repositories](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/apk_repo.html#cce-apk-repo) (Alpine)
- [Manage snaps](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/snap.html#cce-snap) (Ubuntu)
- [Yum repositories](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/yum_repo.html#cce-yum-repo) (RPM-based)
- [Configure Zypper repositories](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/zypper_repo.html#cce-zypper-repo) (OpenSUSE)

#### System monitoring and logging
- Create [Reporting](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/reporting.html#cce-reporting) endpoints
- [Output message when cloud-init finishes](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/final_message.html#cce-final-message)
- [Configure system logging via rsyslog](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/rsyslog.html#cce-rsyslog)

#### Configuration managers

**Ansible**
- [Install Ansible](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/ansible.html#cce-ansible)
- [Configure instance to be managed by Ansible](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/ansible_managed.html#cce-ansible-managed)
- [Configure instance to be an Ansible controller](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/ansible_controller.html#cce-ansible-controller)

**Chef**
- [Install and run Chef recipes](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/chef.html#cce-chef)

**Puppet**
- [Puppet](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/puppet.html#cce-puppet)

**Salt Minion**
- [Salt Minion](https://cloudinit.readthedocs.io/en/latest/reference/yaml_examples/salt_minion.html#cce-salt-minion)

### Cloud-Init Configuration Examples

**Using modern cloud-init.* options:**

```yaml
profiles:
    modern-web-server:
        image: ubuntu/24.04/cloud  # /cloud suffix required for cloud-init
        config:
            # Vendor data for common setup
            cloud-init.vendor-data: |
                #cloud-config
                package_update: true
                package_upgrade: true
                packages:
                  - curl
                  - wget
                  - htop

            # User data for specific configuration
            cloud-init.user-data: |
                #cloud-config
                packages:
                  - nginx
                  - certbot
                users:
                  - name: webadmin
                    sudo: ALL=(ALL) NOPASSWD:ALL
                runcmd:
                  - systemctl enable nginx
                  - systemctl start nginx
```

**Using legacy user.* options:**

```yaml
profiles:
    legacy-server:
        image: ubuntu/20.04/cloud  # /cloud suffix required for cloud-init
        config:
            # Vendor data for common setup
            user.vendor-data: |
                #cloud-config
                package_update: true
                packages:
                  - curl
                  - wget

            # User data for specific configuration
            user.user-data: |
                #cloud-config
                packages:
                  - apache2
                users:
                  - name: admin
                    sudo: ALL=(ALL) NOPASSWD:ALL
```

**Network configuration example:**

```yaml
profiles:
    static-network:
        image: ubuntu/24.04/cloud  # /cloud suffix required for cloud-init
        config:
            cloud-init.network-config: |
                version: 2
                ethernets:
                    eth0:
                        dhcp4: false
                        addresses:
                          - 192.168.1.100/24
                        gateway4: 192.168.1.1
                        nameservers:
                            addresses: [8.8.8.8, 1.1.1.1]
```

**Using file includes with cloud-init:**

```yaml
profiles:
    cloud-init-from-files:
        image: ubuntu/24.04/cloud  # /cloud suffix required for cloud-init
        config:
            # Include cloud-init configuration from external files
            cloud-init.vendor-data: !include vendor-data.yaml
            cloud-init.user-data: <@user-data.yaml
            cloud-init.network-config: !include network-config.yaml
```

**Example vendor-data.yaml:**
```yaml
#cloud-config
package_update: true
package_upgrade: true
packages:
    - git
    - curl
    - wget
    - vim
    - htop
timezone: UTC
locale: en_US.UTF-8
```

**Example user-data.yaml:**
```yaml
#cloud-config
users:
    - name: developer
      passwd: $6$rounds=4096$salt$hash
      lock_passwd: false
      sudo: ALL=(ALL) NOPASSWD:ALL
      shell: /bin/bash
      groups: sudo, docker
      ssh_authorized_keys:
          - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBMuZwNxXpAyxLXJYtAsSK6cVWp0zTSw+6JulOXn2O9

write_files:
    - path: /etc/motd
      content: |
          Welcome to your development instance!
          This instance was configured with cloud-init.

runcmd:
    - systemctl enable ssh
    - systemctl start ssh
    - echo "Instance setup complete" > /var/log/setup.log
```

### Determining Which Configuration Set to Use

To determine whether to use `cloud-init.*` or `user.*` options:

1. **Check image documentation**: Refer to the image's documentation or release notes
2. **Test with a sample instance**: Create a test instance and check which options work
3. **Use image age as guide**: Newer images (Ubuntu 22.04+, Alpine 3.16+) typically support `cloud-init.*`
4. **Check cloud-init version**: Instances with cloud-init 22.1+ generally support the newer format

### Migration from user.* to cloud-init.*

When migrating from older `user.*` configuration to newer `cloud-init.*`:

```yaml
# Old configuration (user.*)
profiles:
    old-style:
        image: ubuntu/20.04/cloud  # /cloud suffix required
        config:
            user.user-data: |
                #cloud-config
                packages: [nginx]
            user.vendor-data: |
                #cloud-config
                package_update: true

# New configuration (cloud-init.*)
profiles:
    new-style:
        image: ubuntu/24.04/cloud  # /cloud suffix required
        config:
            cloud-init.user-data: |
                #cloud-config
                packages: [nginx]
            cloud-init.vendor-data: |
                #cloud-config
                package_update: true
```

The configuration content remains the same; only the key names change from `user.*` to `cloud-init.*`.

## Practical Examples

> **💡 Ready-to-Use Example**: Before diving into these examples, check out the included `web-server-example` profile in `packaging/create-config.yaml` and `packaging/web-server-init.example.yaml` for a complete, working nginx web server deployment. See the [Quick Start section](#quick-start---example-web-server) for details.

### Example 1: Web Application Stack

**create-config.yaml:**
```yaml
defaults:
    image: alpine/edge
    ephemeral: false
    memory: 2
    cpu: 2
    disk: 10
    vm: false

profiles:
    base:
        image: ubuntu/24.04/cloud  # /cloud suffix required for cloud-init
        config:
            user.user-data: !include base-cloud-init.yaml

    web-frontend:
        memory: 4
        cpu: 2
        disk: 20
        config:
            user.user-data: !include frontend-setup.yaml

    api-backend:
        memory: 6
        cpu: 3
        disk: 30
        config:
            security.nesting: true
            user.user-data: <@backend-setup.yaml

    database:
        memory: 8
        cpu: 4
        disk: 50
        config:
            security.privileged: true
            user.user-data: <@database-setup.yaml
```

**Usage:**
```bash
# Create frontend server
ssh +frontend+%base+%web-frontend@host

# Create API backend with extra resources
ssh +api+%base+%api-backend+m8@host

# Create database server
ssh +db+%base+%database@host
```

### Example 2: Development Environment

```bash
# Quick development instance with common tools
ssh +dev+%development+%cloud-init-tools@host

# Ephemeral testing instance
ssh ~test+%minimal+%testing-tools@host

# High-performance development instance
ssh +workstation+%development+m16+c8+d100@host
```

### Example 3: Multi-Environment Deployment

```yaml
profiles:
    production:
        config:
            cloud-init.user-data: !include prod-config.yaml
            user.environment.tier: production

    staging:
        config:
            cloud-init.user-data: !include staging-config.yaml
            user.environment.tier: staging

    nginx-lb:
        image: ubuntu/24.04/cloud  # /cloud suffix required for cloud-init
        memory: 2
        config:
            user.user-data: <@nginx-lb-setup.yaml

    app-server:
        image: ubuntu/24.04/cloud  # /cloud suffix required for cloud-init
        memory: 4
        config:
            user.user-data: <@app-server-setup.yaml
```

**Deployment:**
```bash
# Production load balancer
ssh +prod-lb+%production+%nginx-lb@host

# Production app servers
ssh +prod-app1+%production+%app-server@host
ssh +prod-app2+%production+%app-server@host

# Staging environment
ssh +staging-lb+%staging+%nginx-lb+m1@host
ssh +staging-app+%staging+%app-server+m2@host
```

## Advanced Use Cases

### Dynamic Configuration with Scripts

You can use file includes to inject complex setup scripts:

```yaml
profiles:
    kubernetes-node:
        image: ubuntu/24.04/cloud  # /cloud suffix required for cloud-init
        memory: 8
        cpu: 4
        config:
            security.nesting: true
            security.privileged: true
            user.user-data: |
                #cloud-config
                write_files:
                  - path: /opt/k8s-setup.sh
                    permissions: '0755'
                    content: |
                    #!/bin/bash
                    echo "Starting Kubernetes setup..."
                    # Add more setup commands here
                    echo "Kubernetes setup complete!"
                runcmd:
                  - /opt/k8s-setup.sh
```

### Conditional Configuration

Use multiple profiles for conditional setups:

```yaml
profiles:
    base-ubuntu:
        image: ubuntu/24.04

    base-alpine:
        image: alpine/edge

    gpu-support:
        devices:
            gpu0:
                type: gpu

    storage-optimized:
        devices:
            storage:
                type: disk
                size: 100GB
```

**Usage:**
```bash
# GPU-enabled Ubuntu instance
ssh +gpu-workstation+%base-ubuntu+%gpu-support@host

# Storage-optimized Alpine instance
ssh +storage-server+%base-alpine+%storage-optimized@host
```

### Template Inheritance

Create profile hierarchies:

```yaml
profiles:
    base:
        image: ubuntu/24.04/cloud  # /cloud suffix required for cloud-init
        config:
            user.user-data: !include base-setup.yaml

    web-base:
        memory: 4
        config:
            user.user-data: !include web-common.yaml

    nginx-server:
        config:
            user.user-data: <@nginx-specific.yaml

    apache-server:
        config:
            user.user-data: <@apache-specific.yaml
```

**Usage:**
```bash
# Nginx web server with base configuration
ssh +web1+%base+%web-base+%nginx-server@host

# Apache web server with base configuration
ssh +web2+%base+%web-base+%apache-server@host
```

## Troubleshooting

### Common Issues

1. **File Include Not Found**
   ```
   Error: could not read include file /path/to/file.yaml: no such file or directory
   ```
   - Check file path is correct
   - Verify file exists relative to config file directory or current working directory
   - Use absolute paths if needed

2. **Profile Not Found**
   ```
   Error: profile "web-server" does not exist
   ```
   - Verify profile name is correct in `create-config.yaml`
   - Check YAML syntax and indentation
   - Ensure profile is defined under `profiles:` section

3. **Cloud-Init Not Working**
   ```
   Error: cloud-init configuration ignored or not applied
   ```
   - **Most common cause**: Using wrong image type
   - Use images with `/cloud` suffix (e.g., `ubuntu/24.04/cloud`, `alpine/edge/cloud`)
   - Regular images without `/cloud` suffix don't include cloud-init
   - Check if image supports `cloud-init.*` or `user.*` configuration options
   - Verify cloud-init service is running in the instance: `systemctl status cloud-init`

4. **Invalid Configuration Syntax**
   ```
   Error: yaml: unmarshal errors
   ```
   - Validate YAML syntax using online validators
   - Check indentation (use spaces, not tabs)
   - Verify all strings are properly quoted when needed

5. **Permission Issues**
   ```
   Error: permission denied reading config file
   ```
   - Check file permissions on config files and include files
   - Ensure ssh2incus process has read access to all configuration files

### Debug Tips

1. **Test Configuration Loading**
   ```bash
   # Test configuration parsing
   ssh2incus --dump-create
   ```

2. **Verbose Logging**
   ```bash
   # Enable debug logging
   ssh2incus --debug
   ```

### Best Practices

1. **File Organization**
   - Keep include files in same directory as main config
   - Use descriptive filenames
   - Group related configurations together

2. **Profile Design**
   - Create base profiles for common configurations
   - Use specific profiles for specialized needs
   - Document profile purposes and usage

3. **Security Considerations**
   - Restrict access to configuration files
   - Validate include file contents
   - Use minimal required permissions

4. **Testing**
   - Test profiles with ephemeral instances first
   - Validate configurations before production use
   - Keep backup configurations

## Configuration File Locations

ssh2incus looks for configuration files in the following order:

1. Current working directory: `./create-config.yaml`
2. User config directory: `~/.config/ssh2incus/create-config.yaml`
3. System config directory: `/etc/ssh2incus/create-config.yaml`

The first found configuration file is used. Include files are resolved relative to the directory containing the configuration file that references them.
