#
# Python WAMP client: publisher and caller
#
# Install dependencies:
#     https://github.com/crossbario/autobahn-python/wiki/Autobahn-on-Ubuntu
#
# Run this client:
#    ./pyenv1/bin/python pub_caller.py
#
from __future__ import print_function
from twisted.internet.defer import inlineCallbacks
from autobahn.twisted.wamp import ApplicationSession, ApplicationRunner
from autobahn.twisted.util import sleep
from autobahn.wamp.types import CallOptions

realm = u'realm1'
topic = u'example.hello'
procedure = u'example.add2'
procedure2 = u'example.longop'

class MyComponent(ApplicationSession):

    @inlineCallbacks
    def onJoin(self, details):
        print("session joined")

        # ---- Publisher ----

        for i in range(5):
            msg = u'Testing %d' % i
            print("Publishing:", msg)
            self.publish(topic, msg)
            yield sleep(1)

        # ---- Caller ----

        print('Calling procedure:', procedure)
        try:
            res = yield self.call(procedure, 2, 3)
            print("call result: {}".format(res))
        except Exception as e:
            print("call error: {0}".format(e))


        def on_prog(msg):
            print("here")
            print("partial result: {}".format(msg))

        print('Calling procedure:', procedure2)
        try:
            res = yield self.call(procedure2, 'A',
                                  options=CallOptions(on_progress=on_prog))
            print("call result: {}".format(res))
        except Exception as e:
            print("call error: {0}".format(e))


if __name__ == '__main__':
    runner = ApplicationRunner(url=u"ws://localhost:8000/ws", realm=realm)
    runner.run(MyComponent)
