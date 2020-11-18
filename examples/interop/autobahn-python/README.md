# Autobahn Python Client Examples

The Autobahn python client examples demonstrate basic WAMP functionality when running a non-nexus client against a nexus router.  A nexus client is included to demonstrate interoperability when publishing events, and rpc with and withouth progressive results.

To run the Autobahn Python with nexus examples:

1. Setup the python/autobahn environment.  Running `make` should do that.
2. Run a nexus server from the examples. `cd examples/server; go run server.go`
3. Run the subscriber-callee: `./pyenv/bin/python sub_callee.py`
4. Run the autobahn publisher-caller: `./pyenv/bin/python pub_caller.py`
5. Run the nexus publisher-caller: `go run pub_caller.go`
