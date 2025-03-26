#!/bin/bash
set -eo pipefail

# Create directories for compiler wrappers
mkdir -p /go/cross-compiler/amd64/bin
mkdir -p /go/cross-compiler/arm64/bin

# Create wrapper script for amd64 gcc (filters out -m64 flag)
cat > /usr/local/bin/gcc-wrapper-amd64 << 'EOF'
#!/bin/bash
args=()
for arg in "$@"; do
  case "$arg" in
    -m64)
      # Skip -m64 flag, as it is not needed for cross compiler
      ;;
    *)
      args+=("$arg")
      ;;
  esac
done
exec /usr/bin/x86_64-linux-gnu-gcc "${args[@]}"
EOF
chmod +x /usr/local/bin/gcc-wrapper-amd64

# Create wrapper script for arm64 gcc
cat > /usr/local/bin/gcc-wrapper-arm64 << 'EOF'
#!/bin/bash
args=()
for arg in "$@"; do
  case "$arg" in
    -m64|-m32)
      # Skip machine flags
      ;;
    *)
      args+=("$arg")
      ;;
  esac
done
exec /usr/bin/aarch64-linux-gnu-gcc "${args[@]}"
EOF
chmod +x /usr/local/bin/gcc-wrapper-arm64

# Create wrapper scripts for g++ as well
cat > /usr/local/bin/g++-wrapper-amd64 << 'EOF'
#!/bin/bash
args=()
for arg in "$@"; do
  case "$arg" in
    -m64)
      # Skip -m64 flag
      ;;
    *)
      args+=("$arg")
      ;;
  esac
done
exec /usr/bin/x86_64-linux-gnu-g++ "${args[@]}"
EOF
chmod +x /usr/local/bin/g++-wrapper-amd64

cat > /usr/local/bin/g++-wrapper-arm64 << 'EOF'
#!/bin/bash
args=()
for arg in "$@"; do
  case "$arg" in
    -m64|-m32)
      # Skip machine flags
      ;;
    *)
      args+=("$arg")
      ;;
  esac
done
exec /usr/bin/aarch64-linux-gnu-g++ "${args[@]}"
EOF
chmod +x /usr/local/bin/g++-wrapper-arm64

# Create symlinks to make the wrappers easily accessible
ln -sf /usr/local/bin/gcc-wrapper-amd64 /go/cross-compiler/amd64/bin/gcc
ln -sf /usr/local/bin/g++-wrapper-amd64 /go/cross-compiler/amd64/bin/g++
ln -sf /usr/local/bin/gcc-wrapper-arm64 /go/cross-compiler/arm64/bin/gcc
ln -sf /usr/local/bin/g++-wrapper-arm64 /go/cross-compiler/arm64/bin/g++

echo "Compiler wrappers created successfully."
