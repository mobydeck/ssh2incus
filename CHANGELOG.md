# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.7.0] - 2025-09-25

### Added

#### Instance Creation on Demand
- **Dynamic Instance Creation**: New syntax using `+` and `~` prefixes allows creating Incus instances on-demand via SSH login
- **Persistent Instances**: Use `+` prefix (e.g., `ssh +test01@host`) to create persistent instances that remain until manually deleted
- **Ephemeral Instances**: Use `~` prefix (e.g., `ssh ~test01@host`) to create ephemeral instances that auto-delete on poweroff
- **Configuration Parsing**: Smart login parser supports inline instance configuration:
  - Image selection: `+test01+ubuntu/24.04@host`
  - Resource allocation: `+test01+m4+c2+d20@host` (4GB RAM, 2 CPUs, 20GB disk)
  - Advanced options: `+test01+nest+priv+vm@host` (nested, privileged, VM)
- **Default Templates**: Integration with `/etc/ssh2incus/create-config.yaml` for standardized instance creation defaults

#### Authentication Enhancements
- **Password Authentication**: Added `--password-auth` (`-P`) flag to enable password-based SSH authentication
- **Multi-Factor Authentication**: New `--auth-methods` flag allows configuring authentication method chains (e.g., `"publickey,password"`)
- **Advanced Password Hashing**: Integrated `yescrypt-go` library for secure password hashing and verification

#### Terminal Session Management
- **Persistent Sessions**: Added `/` prefix for persistent terminal sessions that survive SSH disconnections
- **Terminal Multiplexer Support**: Added `--term-mux` (`-T`) flag to choose between `tmux` (default) and `screen` for persistent sessions
- **Built-in tmux Binary**: Embedded static tmux binaries (arm64 and amd64) that are automatically deployed when tmux is not available in instances
- **Automatic Package Installation**: System can now automatically install terminal multiplexers (`tmux` or `screen`) in instances when missing

#### System Package Management
- **Cross-Platform Package Installation**: New `InstallPackages()` function with support for:
  - Debian-based systems (apt-get)
  - RHEL-based systems (dnf/yum)
  - Alpine Linux (apk)
- **Automatic OS Detection**: Smart detection of instance operating systems via `/usr/lib/os-release` parsing
- **Package Manager Integration**: Seamless integration with native package managers including proper environment handling

#### Instance Configuration Management
- **Create Config Support**: New `create-config.yaml` functionality for standardized instance creation profiles
- **Configuration Templates**: Support for instance configuration templates with memory, CPU, VM settings, devices, and config options
- **Fallback Configuration Paths**: Smart configuration loading with multiple fallback paths and relative/absolute path resolution
- **Login Parser**: Advanced login string parsing supporting complex instance creation syntax with multiple configuration options

### Changed

#### Core Infrastructure
- **Go Runtime**: Updated from Go 1.24.2 to Go 1.24.7
- **Incus API**: Updated Incus client from v6.11.0 to v6.16.0 for latest container management features
- **Enhanced Argument Parsing**: Improved command-line argument parsing with proper quoted string support and escape character handling
- **Authentication Flow**: Refactored authentication system to support method chaining and more flexible auth configurations

#### Configuration Management
- **Expanded Server Config**: Enhanced server configuration structure with new options:
  - `TermMux`: Terminal multiplexer selection
  - `PassAuth`: Password authentication toggle
  - `AuthMethods`: Authentication method chain configuration

### Improved

#### User Experience
- **On-Demand Infrastructure**: Create and connect to new instances in a single SSH command without pre-provisioning
- **Smart Package Handling**: Instances automatically get required terminal multiplexer packages installed without manual intervention
- **Better Error Messages**: Enhanced error reporting for authentication failures and configuration issues
- **Flexible Authentication**: Users can now combine multiple authentication methods for enhanced security
- **Intuitive Syntax**: Human-readable instance creation syntax (e.g., `+test+ubuntu/24.04+m2+c2+d10+nest+priv`)

#### Performance & Reliability
- **Optimized Dependencies**: Cleaned up unused dependencies and optimized package imports
- **Better Resource Management**: Improved handling of binary deployments and package installations
- **Enhanced Compatibility**: Better compatibility with different Linux distributions and package managers

### Dependencies

#### Added
- `github.com/openwall/yescrypt-go v1.0.0` - Advanced password hashing
- Various container-related dependencies for enhanced Incus integration

#### Updated
- `github.com/lxc/incus/v6 v6.11.0` → `v6.16.0`
- `github.com/spf13/pflag v1.0.6` → `v1.0.10`
- `github.com/stretchr/testify v1.10.0` → `v1.11.1`
- `golang.org/x/crypto v0.36.0` → `v0.42.0`
- `golang.org/x/sys v0.32.0` → `v0.36.0`

#### Removed
- `github.com/peterh/liner` - No longer needed with new terminal handling

### Technical Details

#### New Command-Line Options
- `--password-auth` / `-P`: Enable password authentication
- `--auth-methods`: Configure authentication method chains
- `--term-mux` / `-T`: Select terminal multiplexer (tmux/screen)

#### New SSH Login Syntax
- `+instance-name`: Create persistent instance with defaults
- `~instance-name`: Create ephemeral instance with defaults
- `/instance-name`: Connect with persistent terminal session
- `+instance+image+options`: Create instance with specific configuration
- Example: `ssh +test01+ubuntu/24.04+m4+c2+d20+nest+priv+vm@host`

#### Instance Creation Options
- **Images**: Any valid Incus image (e.g., `ubuntu/24.04`, `alpine/edge`)
- **Resources**: `mN` (memory GB), `cN` (CPU cores), `dN` (disk GB)
- **Features**: `nest` (nesting), `priv` (privileged), `vm` (virtual machine)
- **Shortcuts**: `n`=nest, `p`=priv, `v`=vm, `e`=ephemeral

#### New Configuration Files
- `/etc/ssh2incus/create-config.yaml`: Instance creation configuration template

#### Architecture Support
- Built-in tmux binaries for both ARM64 and AMD64 architectures
- Cross-platform package management for major Linux distributions

---

## [v0.6.0] - 2025-04-07

Release with core SSH-to-Incus functionality, including:
- Basic SSH server with Incus integration
- Public key authentication
- File transfer support (SCP/SFTP)
- Port forwarding capabilities
- SSH agent forwarding
- Multi-remote support
- Process modes (master/daemon)
- Incus shell access
