#!/usr/bin/env python3
"""Nine seated players: layout and optionally full WebRTC mesh acceptance."""
import asyncio
import json
import os
import time
from pathlib import Path

from playwright.async_api import async_playwright
from smoke import BASE, Client

OUT = Path(os.environ.get('RIVER_ARTIFACT_DIR', '/workspace/scratch/river'))


async def main():
    OUT.mkdir(parents=True, exist_ok=True)
    clients = [Client() for _ in range(9)]
    users = []
    for i, client in enumerate(clients):
        client.request('POST', '/api/auth/guest', {'name': f'玩家 {i + 1}', 'password': os.environ.get('JOIN_PASSWORD', '')})
        users.append(client.request('GET', '/api/me'))
    settings = {'visibility': 'public', 'smallBlind': 5, 'bigBlind': 10, 'buyIn': 1000, 'maxPlayers': 9,
                'actionSeconds': 120, 'voiceEnabled': True, 'voiceMode': 'free',
                'spectatorVoiceEnabled': False, 'chatEnabled': True, 'reactionsEnabled': True}
    room = clients[0].request('POST', '/api/rooms', {'name': '九人满桌验收 ' + str(time.time_ns())[-5:], 'settings': settings})['id']
    for i, client in enumerate(clients):
        await client.connect(room)
        await client.send(type='sit', seat=i)
        await client.wait_state(lambda s: any(p['id'] == users[i]['id'] and p['seat'] == i for p in s['players']))
    async with async_playwright() as p:
        browser = await p.chromium.launch(args=['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'])
        count = 9 if os.environ.get('RIVER_TEST_FULL_VOICE') == '1' else 1
        contexts, pages, errors = [], [], []
        for i in range(count):
            context = await browser.new_context(viewport={'width': 1440, 'height': 1000}, permissions=['microphone'])
            contexts.append(context)
            await context.add_cookies([{'name': c.name, 'value': c.value, 'url': BASE, 'httpOnly': True} for c in clients[i].cookies])
            await context.add_init_script('''window.__riverPeers=[];const Peer=window.RTCPeerConnection;
              window.RTCPeerConnection=class extends Peer {constructor(...args){super(...args);window.__riverPeers.push(this)}};''')
            page = await context.new_page()
            page.on('pageerror', lambda error: errors.append(str(error)))
            await page.goto(BASE + '/?room=' + room)
            await page.get_by_text('已连接', exact=True).wait_for()
            pages.append(page)
        host = pages[0]
        await host.get_by_role('button', name='开始牌局', exact=True).click()
        await host.get_by_text('第 1 手 · 翻牌前', exact=True).wait_for()
        await host.wait_for_timeout(800)
        for width, height, label in [(1440, 1000, 'nine-desktop'), (1024, 768, 'nine-tablet'), (390, 844, 'nine-mobile')]:
            await host.set_viewport_size({'width': width, 'height': height})
            await host.wait_for_timeout(150)
            assert await host.evaluate('document.documentElement.scrollWidth <= innerWidth'), label + ' overflow'
            assert await host.locator('.seat-avatar-button').count() == 9
            boxes = await host.locator('.seat-avatar-button').evaluate_all('(nodes)=>nodes.map(n=>{const b=n.getBoundingClientRect();return{x:b.x,y:b.y,w:b.width,h:b.height}})')
            for box in boxes:
                assert box['x'] >= 0 and box['x'] + box['w'] <= width, label + ' clipped avatar'
            for i, a in enumerate(boxes):
                for b in boxes[i + 1:]:
                    overlap = min(a['x'] + a['w'], b['x'] + b['w']) - max(a['x'], b['x'])
                    vertical = min(a['y'] + a['h'], b['y'] + b['h']) - max(a['y'], b['y'])
                    assert overlap <= 0 or vertical <= 0, label + ' overlapping avatars'
            await host.screenshot(path=str(OUT / (label + '.png')), full_page=True)
        if count == 9:
            await asyncio.gather(*(page.get_by_role('button', name='加入语音', exact=True).click() for page in pages))
            await asyncio.gather(*(page.wait_for_function("window.__riverPeers.filter(p=>p.connectionState==='connected').length===8", timeout=45000) for page in pages))
            for page in pages:
                assert await page.evaluate("[...document.querySelectorAll('audio')].filter(a=>a.srcObject?.getAudioTracks().length>0).length===8"), 'Incomplete received audio mesh'
        assert not errors, errors
        for context in contexts:
            await context.close()
        await browser.close()
    for client in clients:
        await client.ws.close()
    print(json.dumps({'result': 'PASS', 'players': 9, 'layouts': ['desktop', 'tablet', 'portrait mobile'],
                      'fullVoiceMesh': count == 9, 'screenshots': str(OUT)}, ensure_ascii=False))


if __name__ == '__main__':
    asyncio.run(main())
