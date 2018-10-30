# Autobahn Python Client Examples

The Autobahn python client examples demonstrate basic WAMP functionality when running a non-nexus client against a nexus router.

To run the Autobahn Python examples:

1. Setup the python/autobahn environment.  Running `make` should do that.
2. Run a nexus server from the examples.
3. Run the subscriber-callee: `./pyenv/bin/python sub_callee.py`
4. Run the publisher-caller: `./pyenv/bin/python pub_caller.py`
