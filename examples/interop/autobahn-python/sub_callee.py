#
# Python WAMP client: subscriber and callee
#
# Install dependencies:
#     make
#
# Run this client:
#    ./pyenv/bin/python sub_callee.py
#
from __future__ import print_function
from twisted.internet.defer import inlineCallbacks, returnValue
from autobahn.twisted.wamp import ApplicationSession, ApplicationRunner
from autobahn.twisted.util import sleep
from autobahn.wamp.types import RegisterOptions

realm = u'realm1'
topic = u'example.hello'
procedure = u'example.add2'
procedure2 = u'example.longop'

event_count = 0

class MyComponent(ApplicationSession):

    @inlineCallbacks
    def onJoin(self, details):
        print("session joined")

        # ---- Subscriber ----
        def onevent(msg):
            global event_count
            event_count += 1
            print("Got event: {}".format(msg))

        yield self.subscribe(onevent, topic)

        # ---- Callee ----
        def add2(x, y):
            print("Adding", x, "and", y)
            return int(x) + int(y)

        try:
            yield self.register(add2, procedure)
            print(procedure, "registered")
        except Exception as e:
            print("could not register procedure: {0}".format(e))


        @inlineCallbacks
        def longop(a, details=None):
            if details.progress:
                print("Alpha")
                details.progress("Alpha")
                yield sleep(1)

                print("Bravo")
                details.progress("Bravo")
                yield sleep(1)

                print("Charlie")
                details.progress("Charlie")
                yield sleep(1)

            returnValue("ok")

        try:
            yield self.register(longop, procedure2,
                                RegisterOptions(details_arg='details'))
            print(procedure2, "registered")
        except Exception as e:
            print("could not register procedure: {0}".format(e))


if __name__ == '__main__':
    runner = ApplicationRunner(url=u"ws://localhost:8080/ws", realm=realm)
    runner.run(MyComponent)
    print("Received", event_count, "events")
