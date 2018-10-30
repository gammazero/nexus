# Crossbar Router Node Setup

This directory contains a `Makefile` that automates installing and initializing a Crossbar router node.  This may be used for compatibility testing, to test that nexus clients operate correctly with a Crossbar WAMP router.

To setup, configure, and run the Crossbar node:

1. Setup the python/autobahn environment and install Crossbar:  Run `make`
2. Optionally configure the node: edit `node/.crossbar/config.json`
3. Run the node: `pyenv/bin/crossbar start --cbdir node/.crossbar`

Run nexus clients to test with the Crossbar router node.
