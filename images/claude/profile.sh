export PATH="$HOME/.local/bin:$PATH"
# Claude Code's bundled ripgrep is built for glibc. Alpine uses musl.
export USE_BUILTIN_RIPGREP=0
