#!/usr/bin/env python3
"""Real-service acceptance test. pip install websockets; run against an isolated database."""
import asyncio
import http.cookiejar
import json
import os
import time
import urllib.error
import urllib.request

import websockets

BASE = os.environ.get('RIVER_TEST_URL', 'http://localhost:8080').rstrip('/')


class Client:
    def __init__(self):
        self.cookies = http.cookiejar.CookieJar()
        self.http = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))
        self.state = None

    def request(self, method, path, data=None):
        body = json.dumps(data).encode() if data is not None else None
        req = urllib.request.Request(BASE + path, data=body, method=method,
                                     headers={'Content-Type': 'application/json', 'Origin': BASE})
        with self.http.open(req, timeout=10) as response:
            content = response.read()
            return json.loads(content) if content else None

    async def connect(self, room):
        cookie = '; '.join(f'{c.name}={c.value}' for c in self.cookies)
        self.ws = await websockets.connect(BASE.replace('http', 'ws', 1) + f'/api/rooms/{room}/ws',
                                           origin=BASE, additional_headers={'Cookie': cookie})
        return await self.wait_state()

    async def send(self, **data):
        await self.ws.send(json.dumps(data))

    async def wait_state(self, predicate=lambda state: True):
        async with asyncio.timeout(8):
            while True:
                event = json.loads(await self.ws.recv())
                if event['type'] == 'error':
                    raise AssertionError(f"Server error: {event['message']}")
                if event['type'] == 'state':
                    self.state = event['state']
                    if predicate(self.state):
                        return self.state

    async def expect_error(self):
        async with asyncio.timeout(5):
            while True:
                event = json.loads(await self.ws.recv())
                if event['type'] == 'error':
                    return event


async def main():
    clients = [Client() for _ in range(4)]
    stamp = str(time.time_ns())
    users = []
    for i, client in enumerate(clients):
        client.request('POST', '/api/auth/guest', {'name': f'Test {stamp[-6:]} {i}',
                                                 'password': os.environ.get('JOIN_PASSWORD', '')})
        users.append(client.request('GET', '/api/me'))
    settings = {'visibility': 'public', 'smallBlind': 5, 'bigBlind': 10, 'buyIn': 1000, 'maxPlayers': 9,
                'actionSeconds': 30, 'voiceEnabled': True, 'chatEnabled': True, 'reactionsEnabled': True}
    room = clients[0].request('POST', '/api/rooms', {'name': 'Acceptance ' + stamp[-6:], 'settings': settings})['id']
    for client in clients:
        await client.connect(room)
    for i in range(3):
        await clients[i].send(type='sit', seat=i, buyIn=1000)
        await clients[i].wait_state(lambda s: any(p['id'] == users[i]['id'] and p['seat'] == i for p in s['players']))
    await clients[1].send(type='start')
    await clients[1].expect_error()
    await clients[0].send(type='stack', playerId=users[1]['id'], amount=350)
    await clients[0].wait_state(lambda s: next(p for p in s['players'] if p['id'] == users[1]['id'])['stack'] == 350)
    before = sum(p['stack'] for p in clients[0].state['players'])
    await clients[0].send(type='start')
    host = await clients[0].wait_state(lambda s: bool(s.get('hand')) and s['hand']['phase'] == 'preflop')
    spectator = await clients[3].wait_state(lambda s: bool(s.get('hand')) and s['hand']['phase'] == 'preflop')
    assert all(not p['cards'] for p in spectator['hand']['players']), 'Spectator sees private cards'
    assert all(not p['cards'] for p in host['hand']['players'] if p['id'] != users[0]['id']), 'Opponent cards leaked'
    assert len(next(p for p in host['hand']['players'] if p['id'] == users[0]['id'])['cards']) == 2
    await clients[0].send(type='stack', playerId=users[1]['id'], amount=9999)
    await clients[0].expect_error()
    state = host
    for _ in range(12):
        if state['hand']['phase'] in ('complete', 'showdown'):
            break
        turn = state['hand']['turnSeat']
        idx = next(i for i in range(3) if any(p['id'] == users[i]['id'] and p['seat'] == turn for p in state['players']))
        await clients[idx].send(type='action', action='allin', turnToken=state['hand']['turnToken'])
        previous = state['version']
        state = await clients[0].wait_state(lambda s: s['version'] > previous)
    assert state['hand']['phase'] in ('complete', 'showdown'), state
    assert len(state['hand']['board']) == 5
    assert state['hand'].get('winners'), 'Missing settlement'
    after = sum(p['stack'] for p in state['players'])
    assert before == after, f'Chip conservation failed: {before} -> {after}'
    await clients[2].send(type='chat', text='边池测试完成')
    await clients[0].wait_state(lambda s: any(m['text'] == '边池测试完成' for m in s['messages']))
    disconnected = Client()
    disconnected.cookies = clients[0].cookies
    disconnected.http = clients[0].http
    await clients[0].ws.close()
    restored = await disconnected.connect(room)
    assert restored['hand']['number'] == state['hand']['number']
    for client in clients[1:]:
        await client.ws.close()
    await disconnected.ws.close()
    print(json.dumps({'result': 'PASS', 'room': room, 'checks': ['guest sessions', 'host permissions',
                     'three-player all-in and side pots', 'private cards', 'chip conservation',
                     'mid-hand stack protection', 'chat', 'reconnect']}, ensure_ascii=False))


if __name__ == '__main__':
    asyncio.run(main())
