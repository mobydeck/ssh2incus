name := "ssh2incus"
# Get version from git tags
version := `git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev"`
edition := "ce"
githash := `git rev-parse --short HEAD || echo`
release := "0"
builtat := `date -u +"%Y-%m-%dT%H:%M:%SZ"`

sysgroups := "incus"


# Default recipe to show available commands
default:
    @just --list

# Build flags with version information
build_flags := "-trimpath -ldflags='-s -w -extldflags static -X " + name + ".version=" + version + " -X " + name + ".githash=" + githash + " -X " + name + ".builtat=" + builtat + " -X " + name + ".flagGroups=" + sysgroups + "'"

# Show current version
version:
    @echo {{version}}

build:
    just build-sftp-server amd64
    just build-sftp-server arm64

# Build for a specific platform and architecture
build-for os arch:
    @echo "Building for {{os}} ({{arch}}) version {{version}}..."
    @mkdir -p dist
    CGO_ENABLED=0 GOOS={{os}} GOARCH={{arch}} \
    go build {{build_flags}} \
        -o ./dist/{{name}}-{{os}}-{{arch}} \
        cmd/{{name}}/{{name}}.go

build-sftp-server-all:
    just build-sftp-server amd64
    just build-sftp-server arm64

build-sftp-server arch:
    mkdir -p server/bin
    CGO_ENABLED=0 GOOS=linux GOARCH={{arch}} \
        go build -trimpath -ldflags="-s -w -extldflags static" \
        -o ./server/bin/{{name}}-sftp-server-{{arch}} \
        cmd/sftp-server/sftp-server.go
    gzip -9 -f -k ./server/bin/{{name}}-sftp-server-{{arch}}

build-all: clean create-dist
    just build-sftp-server-all
    just build-for linux amd64
    just build-for linux arm64
    just build-for darwin amd64
    just build-for darwin arm64
    @echo "All builds completed successfully!"
    @ls -lah dist/

# Create distribution directory
create-dist:
    mkdir -p dist

# Clean build artifacts
clean:
    rm -rf dist
    rm -f {{name}} {{name}}.exe

# Create archive for a specific build
create-archive os arch:
    @echo "Creating archive for {{os}}/{{arch}} with documentation..."
    @cp README.md dist/
    @if [ -f LICENSE ]; then cp LICENSE dist/; fi
    @if [ "{{os}}" = "windows" ]; then \
        cd dist && zip {{name}}-{{version}}-{{os}}-{{arch}}.zip {{name}}-{{os}}-{{arch}}.exe README.md $([ -f LICENSE ] && echo "LICENSE"); \
    else \
        cd dist && tar -czf {{name}}-{{version}}-{{os}}-{{arch}}.tar.gz {{name}}-{{os}}-{{arch}} README.md $([ -f LICENSE ] && echo "LICENSE"); \
    fi
    @rm -f dist/README.md dist/LICENSE 2>/dev/null
    @echo "✓ Archive created for {{os}}/{{arch}}"

# Create release archives
package: build-all
    @echo "Creating release archives..."
    just create-archive linux amd64
    just create-archive linux arm64
    just create-archive darwin amd64
    just create-archive darwin arm64

    just create-package rpm amd64
    just create-package deb amd64
    just create-package rpm arm64
    just create-package deb arm64

    @echo "✓ Created release archives in dist/"
    @echo "✓ Created release packages in release/"

create-package pkg arch:
    rm -rf build
    mkdir -p {build,release}
    cp dist/{{name}}-linux-{{arch}} build/{{name}}
    VERSION={{version}} RELEASE={{release}} ARCH={{arch}} nfpm pkg -p {{pkg}} -f ./packaging/nfpm.yaml -t ./release

# Create a GitHub release and upload distribution files
release: package
    #!/bin/sh -e
    echo "Creating GitHub release for version {{version}}..."
    if [ "$(git status --porcelain)" != "" ]; then \
        echo "Error: Working directory is not clean. Commit or stash changes before creating a release."; \
        exit 1; \
    fi
    if [ "$(git branch --show-current)" != "main" ] && [ "$(git branch --show-current)" != "master" ]; then \
        echo "Warning: You are not on main/master branch. Continue? [y/N]"; \
        read -r answer; \
        if [ "$answer" != "y" ] && [ "$answer" != "Y" ]; then \
            echo "Release cancelled."; \
            exit 1; \
        fi; \
    fi
    echo "Checking for GitHub token..."
    if [ -z "$GH_TOKEN" ]; then \
        echo "GH_TOKEN not set, attempting to extract from .netrc file..."; \
        if [ ! -f ~/.netrc ]; then \
            echo "Error: ~/.netrc file not found and GH_TOKEN not set."; \
            echo "Please set GH_TOKEN environment variable or create .netrc with GitHub credentials."; \
            exit 1; \
        fi; \
        GITHUB_TOKEN=$(grep "machine github.com" ~/.netrc | grep "password" | awk '{print $6}') && \
        if [ -z "$GITHUB_TOKEN" ]; then \
            echo "Error: GitHub token not found in .netrc file."; \
            echo "Please ensure your .netrc contains a 'machine github.com' entry with a password or set GH_TOKEN."; \
            exit 1; \
        fi; \
        export GH_TOKEN="$GITHUB_TOKEN"; \
        echo "GitHub token extracted successfully."; \
    else \
        echo "Using existing GH_TOKEN environment variable."; \
    fi
    echo "Finding archive files in dist folder..."
    FILES=$(find {dist,release} -type f -name "*.tar.gz" -o -name "*.zip" -o -name "*.deb" -o -name "*.rpm" | tr '\n' ' ') && \
    if [ -z "$FILES" ]; then \
        echo "Error: No archive files found in dist folder"; \
        exit 1; \
    else \
        echo "Found archive files: $FILES"; \
        gh release create {{version}} \
            --title "Release {{version}}" \
            --notes "Release {{version}}" \
            --draft \
            $FILES; \
    fi
    echo "✓ Created draft release {{version}} on GitHub"
    echo "Review and publish the release at: https://github.com/$(gh repo view --json nameWithOwner -q .nameWithOwner)/releases"
