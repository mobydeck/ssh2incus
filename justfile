set export := true

name := "ssh2incus"

# Get version from git tags

version := `git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev"`
draft_version := `git describe --tags --always 2>/dev/null || echo "0.0.0-dev"`
edition := "ce"
githash := `git rev-parse --short HEAD || echo`
release := "0"
builtat := `date -u +"%Y-%m-%dT%H:%M:%SZ"`
sysgroups := "incus,incus-admin"
common_build_flags := "-trimpath -ldflags="
common_ldflags := "-s -w -extldflags static"
build_flags := common_build_flags + "'" + common_ldflags + "'"
version_ldflag := " -X " + name + ".version=" + version
githash_ldflag := " -X " + name + ".gitHash=" + githash
builtat_ldflag := " -X " + name + ".builtAt=" + builtat
proxy_device_prefix_flag := " -X " + name + "/pkg/incus.ProxyDevicePrefix=" + name + "-proxy"
main_build_flags := common_build_flags + "'" + common_ldflags + version_ldflag + githash_ldflag + builtat_ldflag + proxy_device_prefix_flag + "'"

# Default recipe to show available commands
default:
    @just --list

# Show current version
version:
    @echo {{ version }}

tag tag:
    git tag {{ tag }} HEAD -f
    git push --tags -f

test:
    go test ./...

generate:
    echo "# Arguments to pass to {{ name }} daemon" > ./packaging/{{ name }}.env
    echo "ARGS=-m -W" >> ./packaging/{{ name }}.env
    echo >> ./packaging/{{ name }}.env
    go run ./cmd/{{ name }} -h | awk 'NR > 2 && NR < length {print "#" $0}' >> ./packaging/{{ name }}.env
    go generate ./...

build:
    just build-sftp-server-all
    just build-stdio-proxy-all

# Build for a specific platform and architecture
build-for os arch:
    @echo "Building for {{ os }} ({{ arch }}) version {{ version }}..."
    @mkdir -p dist
    CGO_ENABLED=0 GOOS={{ os }} GOARCH={{ arch }} \
    go build {{ main_build_flags }} \
        -o ./dist/{{ name }}-{{ os }}-{{ arch }} \
        cmd/{{ name }}/{{ name }}.go

build-sftp-server-all:
    just build-sftp-server amd64
    just build-sftp-server arm64

build-sftp-server arch:
    mkdir -p server/sftp-server-binary/bin
    CGO_ENABLED=0 GOOS=linux GOARCH={{ arch }} \
        go build {{ build_flags }} \
        -o ./server/sftp-server-binary/bin/{{ name }}-sftp-server-{{ arch }} \
        cmd/sftp-server/sftp-server.go
    gzip -9 -f -k ./server/sftp-server-binary/bin/{{ name }}-sftp-server-{{ arch }}

build-stdio-proxy-all:
    just build-stdio-proxy amd64
    just build-stdio-proxy arm64

build-stdio-proxy arch:
    mkdir -p server/stdio-proxy-binary/bin
    CGO_ENABLED=0 GOOS=linux GOARCH={{ arch }} \
        go build {{ build_flags }} \
        -o ./server/stdio-proxy-binary/bin/{{ name }}-stdio-proxy-{{ arch }} \
        cmd/stdio-proxy/stdio-proxy.go
    gzip -9 -f -k ./server/stdio-proxy-binary/bin/{{ name }}-stdio-proxy-{{ arch }}

npm-updates:
    npx npm-check-updates

download-tmux-all:
    just download-tmux amd64
    just download-tmux arm64

download-tmux arch:
    mkdir -p server/tmux-binary/bin
    curl -o server/tmux-binary/bin/{{ name }}-tmux-{{ arch }}.gz -L https://github.com/arthurpro/tmux-static-build/releases/download/3.5a/tmux.linux-{{ arch }}.gz

build-all: clean build-web create-dist generate test
    just build-sftp-server-all
    just build-stdio-proxy-all
    just build-for linux amd64
    just build-for linux arm64
    just build-for darwin amd64
    just build-for darwin arm64
    @echo "All builds completed successfully!"
    @ls -lah dist/

build-web:
    npm --prefix web run build

# Create distribution directory
create-dist:
    mkdir -p dist

# Clean build artifacts
clean:
    rm -rf dist release
    rm -f {{ name }} {{ name }}.exe

# Create archive for a specific build
create-archive os arch:
    @echo "Creating archive for {{ os }}/{{ arch }} with documentation..."
    @cp README.md dist/
    @if [ -f LICENSE ]; then cp LICENSE dist/; fi
    @if [ "{{ os }}" = "windows" ]; then \
        cd dist && zip {{ name }}-{{ version }}-{{ os }}-{{ arch }}.zip {{ name }}-{{ os }}-{{ arch }}.exe README.md $([ -f LICENSE ] && echo "LICENSE"); \
    else \
        cd dist && tar -czf {{ name }}-{{ version }}-{{ os }}-{{ arch }}.tar.gz {{ name }}-{{ os }}-{{ arch }} README.md $([ -f LICENSE ] && echo "LICENSE"); \
    fi
    @rm -f dist/README.md dist/LICENSE 2>/dev/null
    @echo "✓ Archive created for {{ os }}/{{ arch }}"

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
    cp dist/{{ name }}-linux-{{ arch }} build/{{ name }}
    VERSION={{ version }} RELEASE={{ release }} ARCH={{ arch }} nfpm pkg -p {{ pkg }} -f ./packaging/nfpm.yaml -t ./release

# Create a GitHub release and upload distribution files
release: clean package
    #!/bin/sh -e
    echo "Creating GitHub release for version {{ version }}..."
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
        GITHUB_TOKEN=$(grep "machine github.com" ~/.netrc | grep "password" | awk '{print $6}' | head -1) && \
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
        gh release create {{ version }} \
            --title "Release {{ version }}" \
            --notes "Release {{ version }}" \
            --draft \
            $FILES; \
    fi
    echo "✓ Created draft release {{ version }} on GitHub"
    echo "Review and publish the release at: https://github.com/$(gh repo view --json nameWithOwner -q .nameWithOwner)/releases"

# Create a GitHub draft release and upload tar.gz from dist/ and rpm/deb from release/ folders
draft-release:
    #!/bin/sh -e
    echo "Creating GitHub draft release for version {{ draft_version }}..."

    # Extract GitHub token from .netrc
    echo "Checking for GitHub token..."
    if [ -z "$GH_TOKEN" ]; then \
        echo "GH_TOKEN not set, attempting to extract from .netrc file..."; \
        if [ ! -f ~/.netrc ]; then \
            echo "Error: ~/.netrc file not found and GH_TOKEN not set."; \
            echo "Please set GH_TOKEN environment variable or create .netrc with GitHub credentials."; \
            exit 1; \
        fi; \
        GITHUB_TOKEN=$(grep "machine github.com" ~/.netrc | grep "password" | awk '{print $6}' | head -1) && \
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

    # Collect distribution files
    echo "Collecting distribution files..."
    TAR_GZ_FILES=$(find dist -type f -name "*.tar.gz" 2>/dev/null | sort)
    DEB_FILES=$(find release -type f -name "*.deb" 2>/dev/null | sort)
    RPM_FILES=$(find release -type f -name "*.rpm" 2>/dev/null | sort)

    if [ -z "$TAR_GZ_FILES" ] && [ -z "$DEB_FILES" ] && [ -z "$RPM_FILES" ]; then \
        echo "Error: No distribution files found in dist/ or release/ folders"; \
        exit 1; \
    fi

    # List files to be uploaded
    if [ -n "$TAR_GZ_FILES" ]; then \
        echo "tar.gz files:"; \
        echo "$TAR_GZ_FILES" | sed 's/^/  - /'; \
    fi

    if [ -n "$DEB_FILES" ]; then \
        echo "deb files:"; \
        echo "$DEB_FILES" | sed 's/^/  - /'; \
    fi

    if [ -n "$RPM_FILES" ]; then \
        echo "rpm files:"; \
        echo "$RPM_FILES" | sed 's/^/  - /'; \
    fi

    # Create draft release
    echo ""
    echo "Creating draft release on GitHub..."
    gh release create {{ draft_version }} \
        --title "Release {{ draft_version }}" \
        --notes "Release {{ draft_version }}" \
        --draft \
        --target $(git log -n 1 --pretty=format:"%H" main) \
        $TAR_GZ_FILES $DEB_FILES $RPM_FILES

    echo ""
    echo "✓ Created draft release {{ draft_version }} on GitHub"
    REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
    echo "Review and publish the release at: https://github.com/$REPO/releases/tag/{{ draft_version }}"
