#
# Python WAMP client: callee that handles RPC from echo_caller.py
#
# Install dependencies:
#     make
#
# Run this client:
#    ./pyenv/bin/python echo_callee.py
#
import asyncio
from autobahn.asyncio.wamp import ApplicationSession
from autobahn.asyncio.component import Component, run


class Backend(ApplicationSession):
    def __init__(self, config=None):
        config.realm = "realm1"
        super().__init__(config)

    async def onJoin(self, details):
        print('Register test_echo_payload')
        await self.register(self.test_echo_payload, 'test.echo.payload')

    async def onDisconnect(self):
        loop  = asyncio.get_event_loop()
        loop.stop()

    async def test_echo_payload(self, value: bytes) -> bytes:
        if not isinstance(value, bytes):
            print('Value is not an instance of bytes but is a %s' % (type(value)))
        return value


if __name__ == '__main__':
    backend = Component(
            session_factory = Backend,
            transports      = [
                {
                    'type':        'websocket',
                    'serializers': ['msgpack'],
                    'url':         'ws://localhost:8080/ws',
                    'max_retries': 3
                }
            ]
        )

    run([backend])
