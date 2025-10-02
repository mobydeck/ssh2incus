# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.8.0 — 2025-10-02

### Added

#### Configuration File Support
- **YAML Configuration**: New `config.yaml` file support for persistent server configuration
  - Configuration file loads from current directory, `$HOME/.config/ssh2incus/`, or `/etc/ssh2incus/` (checked in order)
  - All configuration options available as YAML settings with same names as command-line flags
  - All options commented out by default to use system defaults
  - Command-line flags have higher priority than configuration file options
- **Flexible Configuration Management**: Simplified server configuration without modifying system service files
  - Each YAML setting maps directly to corresponding command-line flag
  - Easy to enable/disable features by uncommenting configuration options
  - Better configuration organization and documentation

#### Enhanced Instance Creation Configuration
- **Profile-Based Instance Creation**: New `%profile` syntax allows applying predefined configuration profiles during instance creation
  - Use `%profile1+%profile2` in login string (e.g., `ssh +instance+%web-server+%database@host`)
  - Profiles are applied in order with later profiles overriding earlier ones
  - Direct configuration options always override profile settings
- **File Include Support**: Configuration files now support external file includes
  - `!include filename.ext` syntax for loading file contents into configuration values
  - `<@filename.ext` alternative syntax for file includes
  - Smart path resolution: first tries relative to config file directory, then current working directory
- **Advanced Configuration Templates**: Enhanced `create-config.yaml` with profile support
  - New `profiles` section for defining reusable configuration templates
  - Hierarchical configuration resolution: defaults → profiles → direct options
  - Support for complex multi-profile scenarios

#### Instance Creation Workflow Improvements
- **Configuration Override Hierarchy**: Clear precedence order for configuration resolution
  - Base defaults from `create-config.yaml`
  - Applied profiles in specified order
  - Direct SSH login string options (highest priority)
- **Enhanced Login String Parsing**: Improved parsing of complex instance creation syntax
  - Support for multiple profiles: `+instance+%profile1+%profile2+options@host`
  - Better error handling for malformed login strings
  - Validation of profile existence before instance creation

#### Built-in SFTP Server Enhancements
- **CHROOT Support**: New `-c` flag enables chrooting to the start directory for enhanced security isolation
- **Directory Control**: Enhanced `-d` flag for setting custom start directories in SFTP sessions
- **Security Improvements**: Better privilege separation and directory access control

#### SSH Banner and Welcome Message Customization
- **Custom Banner Support**: Server now looks for `banner.txt` file to display custom SSH login banners
- **Welcome Message**: Optional `welcome.txt` file provides personalized welcome messages for users
- **Template Variables**: Both banner and welcome files support template variables:
  - `[INSTANCE_USER]`: Current instance user
  - `[INSTANCE]`: Instance name
  - `[PROJECT]`: Project name
  - `[REMOTE]`: Remote server name
  - `[HOSTNAME]`: System hostname
- **Example Files**: Provided `banner.txt.example` and `welcome.txt.example` templates in packaging

#### Improved Login String Parsing
- **Enhanced Parser**: Completely refactored login string parsing with better modularity
- **Comprehensive Testing**: Extensive test coverage for all login string formats and edge cases
- **Better Error Handling**: Improved validation and error reporting for malformed login strings
- **Backward Compatibility**: Maintained full compatibility with existing login string formats

### Changed

#### Configuration System
- **Extended CreateConfig Structure**: Enhanced configuration file format
  - Added `profiles` map for named configuration templates
  - Improved validation and error reporting for configuration files
  - Better handling of optional configuration sections
- **Enhanced File Processing**: Improved `LoadCreateConfig` function
  - Added file include processing for both defaults and profile configurations
  - Better error messages for missing include files or invalid paths
  - Support for nested configuration scenarios

#### SFTP Server Implementation
- **Command-line Flags**: Added support for standard OpenSSH SFTP server flags (-c, -d, -R, -e, -u, -l, -h)
- **Security Model**: Enhanced security with proper chroot and directory change operations
- **Environment Integration**: Better integration with UID/GID environment variables

#### Login String Processing
- **Modular Architecture**: Split parsing logic into focused, testable functions
- **Performance Improvements**: Optimized parsing for complex login string formats
- **Code Organization**: Better separation of concerns for different login string components

### Improved

#### User Experience
- **Intuitive Profile Usage**: Simple syntax for applying complex configurations
  - Example: `ssh +web01+%nginx+%ssl+ubuntu/24.04@host` applies nginx and SSL profiles with Ubuntu 24.04
- **Flexible Configuration Management**: Easy organization of instance templates
  - Separate profile files can be included via file include directives
  - Configuration inheritance allows for base profiles with specialized extensions
- **Better Error Handling**: Enhanced error messages for configuration issues
  - Clear indication when profiles are missing or invalid
  - Better path resolution error reporting for file includes
- **Visual Feedback**: Custom banners provide better visual identification of servers and instances
- **Personalization**: Welcome messages can be customized per deployment
- **Security**: SFTP chroot functionality provides better file access isolation

#### Development & Maintenance
- **Modular Configuration**: Profile-based system enables better configuration organization
- **Template Reusability**: Profiles can be shared across different instance creation scenarios
- **Configuration Validation**: Enhanced validation ensures configuration consistency

### Examples

#### Profile-Based Instance Creation
```bash
# Create instance with web-server profile
ssh -p 2222 +web01+%web-server@host

# Create instance with multiple profiles (database settings override web-server)
ssh -p 2222 +app01+%web-server+%database@host

# Override profile settings with direct options
ssh -p 2222 +dev01+%development+m16+c8@host
```

#### Configuration File with Profiles
```yaml
version: 1
defaults:
  image: alpine/edge
  memory: 1
  cpu: 1

profiles:
  web-server:
    image: ubuntu/24.04
    memory: 2
    cpu: 2
    config:
      user.user-data: "!include web-server-init.yaml"

  database:
    memory: 4
    cpu: 2
    config:
      user.user-data: "<@database-setup.sh"
```

### Technical Details

#### New Configuration Processing
- File includes processed after YAML unmarshaling but before instance creation
- Profile merging follows last-wins precedence for conflicting settings
- Path resolution tries config directory first, then current working directory
- Enhanced error reporting with specific failure contexts

#### SFTP Server Flags
- `-c`: Enable chroot to start directory
- `-d DIR`: Set start directory
- `-R`: Read-only mode
- `-e`: Debug to stderr
- `-u UMASK`: Set explicit umask
- `-l LEVEL`: Debug level (ignored for compatibility)
- `-h`: Show help

#### Banner and Welcome File Locations
- Files are searched in standard configuration directories
- Template variable substitution occurs at runtime
- Graceful fallback when files are not present

---

## v0.7.0 — 2025-09-25

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

## v0.6.0 — 2025-04-07

Release with core SSH-to-Incus functionality, including:
- Basic SSH server with Incus integration
- Public key authentication
- File transfer support (SCP/SFTP)
- Port forwarding capabilities
- SSH agent forwarding
- Multi-remote support
- Process modes (master/daemon)
- Incus shell access
