#!/usr/bin/env python3
"""Prepare a live hand; restart backend; verify snapshot+session survive. Use isolated database."""
import asyncio
import json
import os
import sys
import time
import urllib.request
from pathlib import Path

from smoke import BASE, Client


async def prepare(path):
    players = [Client(), Client()]
    for i, player in enumerate(players):
        player.request('POST', '/api/auth/guest', {'name': f'Recovery {time.time_ns() % 1000000} {i}',
                                                  'password': os.environ.get('JOIN_PASSWORD', '')})
    settings = {'smallBlind': 5, 'bigBlind': 10, 'buyIn': 1000, 'maxPlayers': 9,
                'actionSeconds': 120, 'voiceEnabled': True, 'chatEnabled': True, 'reactionsEnabled': True}
    room = players[0].request('POST', '/api/rooms', {'name': 'Restart acceptance', 'settings': settings})['id']
    for i, player in enumerate(players):
        await player.connect(room)
        await player.send(type='sit', seat=i)
        await player.wait_state(lambda s: len([p for p in s['players'] if p['seat'] >= 0]) == i + 1)
    await players[0].send(type='start')
    state = await players[0].wait_state(lambda s: bool(s['hand']))
    fixture = {'room': room, 'hand': state['hand'],
               'stacks': {p['id']: p['stack'] for p in state['players']},
               'cookie': '; '.join(f'{c.name}={c.value}' for c in players[0].cookies)}
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, 'w') as file:
        json.dump(fixture, file)
    for player in players:
        await player.ws.close()
    print('Prepared in-progress hand for restart verification')


def verify(path):
    fixture = json.loads(Path(path).read_text())
    request = urllib.request.Request(BASE + '/api/rooms/' + fixture['room'],
                                     headers={'Cookie': fixture['cookie'], 'Origin': BASE})
    with urllib.request.urlopen(request) as response:
        state = json.load(response)
    assert state['hand'] == fixture['hand'], 'Hand, private cards, deadline or turn token changed across restart'
    assert {p['id']: p['stack'] for p in state['players']} == fixture['stacks'], 'Stacks changed across restart'
    assert all(not p['connected'] for p in state['players']), 'Recovered phantom connections'
    Path(path).unlink()
    print('PASS: live hand, private cards, opaque session, stacks, deadline and turn token survived process restart')


if __name__ == '__main__':
    if len(sys.argv) != 3 or sys.argv[1] not in ('prepare', 'verify'):
        raise SystemExit('Usage: recovery_check.py prepare|verify /private/fixture.json')
    if sys.argv[1] == 'prepare':
        asyncio.run(prepare(sys.argv[2]))
    else:
        verify(sys.argv[2])
