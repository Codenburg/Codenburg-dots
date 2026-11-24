# Debian 12 APT configuration fixtures

Captured on Debian 12 amd64 with APT 2.6.1, apt-listchanges 3.24, and debconf 1.5.82. The transferred archive and file manifest were checked before deriving these fixtures.

- The five native fragments, `listchanges.conf`, and `versions.txt` are byte-for-byte copies. `listchanges.conf.d` was empty. The files are test data; do not load them into the host's APT configuration.
- `apt-config-dump.txt` retains the complete key set and order. The only substitutions are the two public changelog hostnames, replaced with `debian.example.invalid` and `ubuntu.example.invalid`.
- This is an `apt-config` snapshot, not an effective `apt-get` configuration or hook trace. It does not establish host file trust, hook protocol behavior, or transaction safety.
