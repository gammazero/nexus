#
# Python WAMP client: caller that calls RPC registered by echo_callee.py
#
# Install dependencies:
#     make
#
# Run this client:
#    ./pyenv/bin/python echo_caller.py
#
import asyncio
from autobahn.asyncio.wamp import ApplicationSession
from autobahn.asyncio.component import Component, run


class Client(ApplicationSession):
    def __init__(self, config=None):
        config.realm = "realm1"
        super().__init__(config)

    async def onJoin(self, details):
        print('Call test_echo_payload')
        loop = asyncio.get_running_loop()
        loop.create_task(self.test_echo_payload())

    async def onDisconnect(self):
        loop  = asyncio.get_event_loop()
        loop.stop()

    async def test_echo_payload(self):
        message_bytes = b'\x01\x02\x03\x04\x05\x06\x07\x08\x09\x10'
        ret = await self.call('test.echo.payload', message_bytes)
        print("Received response:", ret)


if __name__ == '__main__':
    client = Component(
            session_factory = Client,
            transports      = [
                {
                    'type':        'websocket',
                    'serializers': ['msgpack'],
                    'url':         'ws://localhost:8080/ws',
                    'max_retries': 3
                }
            ]
        )

    run([client])
