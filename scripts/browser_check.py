#!/usr/bin/env python3
"""Browser acceptance on a running isolated RIVER service. pip install playwright."""
import json
import os
import time
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

BASE = os.environ.get('RIVER_TEST_URL', 'http://localhost:8080')
OUT = Path(os.environ.get('RIVER_ARTIFACT_DIR', '/workspace/scratch/river'))
OUT.mkdir(parents=True, exist_ok=True)


def assert_width(page):
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'Horizontal overflow'


with sync_playwright() as p:
    browser = p.chromium.launch(args=['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'])
    contexts = [browser.new_context(viewport={'width': 1440, 'height': 1000}, permissions=['microphone']),
                browser.new_context(viewport={'width': 390, 'height': 844}, is_mobile=True, has_touch=True,
                                    permissions=['microphone'])]
    pages = []
    errors = []
    stamp = str(time.time_ns())[-6:]
    for i, context in enumerate(contexts):
        context.add_init_script('''window.__riverPeers=[]; const Peer=window.RTCPeerConnection;
          window.RTCPeerConnection=class extends Peer { constructor(...args){super(...args);window.__riverPeers.push(this)} };''')
        page = context.new_page()
        page.on('pageerror', lambda error: errors.append(str(error)))
        pages.append(page)
        page.goto(BASE)
        page.get_by_label('你的昵称').fill('浏览器' + stamp + str(i))
        if os.environ.get('JOIN_PASSWORD'):
            page.get_by_label('全站加入密码').fill(os.environ['JOIN_PASSWORD'])
        page.get_by_role('button', name='以访客身份进入', exact=True).click()
        page.get_by_role('heading', name='今晚，在哪一桌？').wait_for()
        assert_width(page)
    host, guest = pages
    host.get_by_role('button', name='创建牌桌', exact=True).click()
    dialog = host.get_by_role('dialog')
    dialog.get_by_label('牌桌名称').fill('浏览器验收 ' + stamp)
    dialog.get_by_label('座位数').fill('9')
    dialog.get_by_role('button', name='创建牌桌', exact=True).click()
    host.get_by_role('button', name='坐入 1 号座位', exact=True).wait_for()
    room_url = host.url
    guest.goto(room_url)
    for i, page in enumerate(pages):
        page.get_by_role('button', name=f'坐入 {i+1} 号座位', exact=True).click()
        page.get_by_role('dialog').get_by_role('button', name='确认入座').click()
        page.get_by_role('button', name='离座', exact=True).wait_for()
    host.get_by_role('button', name='开始牌局', exact=True).click()
    host.get_by_text('第 1 手 · 翻牌前', exact=True).wait_for()
    guest.get_by_text('第 1 手 · 翻牌前', exact=True).wait_for()
    expect(host.locator('.card-face:visible')).to_have_count(2)
    expect(guest.locator('.card-face:visible')).to_have_count(2)
    for page in pages:
        assert_width(page)
    host.wait_for_timeout(650)
    host.screenshot(path=str(OUT / 'desktop.png'), full_page=True)
    guest.screenshot(path=str(OUT / 'mobile.png'), full_page=True)
    host.locator('.chat-toggle').click()
    host.get_by_label('聊天消息').fill('两端聊天验证')
    host.get_by_role('button', name='发送消息', exact=True).click()
    guest.locator('.thought-bubble').get_by_text('两端聊天验证', exact=True).wait_for()
    for page in pages:
        page.get_by_role('button', name='加入语音', exact=True).click()
        page.get_by_role('button', name='麦克风已静音', exact=True).wait_for()
    for page in pages:
        page.wait_for_function("window.__riverPeers.some(p=>p.connectionState==='connected')", timeout=15000)
        assert page.evaluate("[...document.querySelectorAll('audio')].some(a=>a.srcObject?.getAudioTracks().length > 0)"), 'No received audio track'
    for _ in range(3):
        acted = False
        for page in pages:
            action = page.get_by_role('button', name='ALL IN', exact=True)
            if action.is_visible() and action.is_enabled():
                action.click()
                page.wait_for_timeout(200)
                acted = True
        if host.get_by_text('第 1 手 · 本手结束', exact=True).is_visible():
            break
        assert acted, 'No player can act'
    host.get_by_text('第 1 手 · 本手结束', exact=True).wait_for()
    host.get_by_role('button', name='房间设置', exact=True).click()
    host.get_by_role('dialog').get_by_label('小盲注').fill('15')
    host.get_by_role('dialog').get_by_label('大盲注').fill('30')
    host.get_by_role('dialog').get_by_role('button', name='保存房间设置').click()
    guest.get_by_text('盲注 15 / 30', exact=True).wait_for()
    host.get_by_role('button', name=f'浏览器{stamp}0 的互动菜单', exact=True).click()
    host.get_by_role('dialog').get_by_role('button', name='🤔', exact=True).click()
    guest.locator('.thought-bubble').get_by_text('🤔', exact=True).wait_for()
    guest.locator('.avatar-emoji').get_by_text('🤔', exact=True).wait_for()
    host.locator('.profile-trigger').click()
    host.get_by_role('dialog').get_by_label('音效音量').fill('0.4')
    host.get_by_role('dialog').get_by_role('button', name='保存设置', exact=True).click()
    assert host.request.get(BASE + '/api/me').json()['settings']['avatarEmoji'] == '🤔'
    host.get_by_role('button', name=f'浏览器{stamp}1 的互动菜单', exact=True).click()
    host.get_by_role('dialog').get_by_role('button', name='🔥', exact=True).click()
    guest.locator('.flying-emoji').get_by_text('🔥', exact=True).wait_for()
    host.locator('.chat-toggle').click()
    for width, height, name in [(768, 1024, 'tablet'), (375, 812, 'mobile-small')]:
        host.set_viewport_size({'width': width, 'height': height})
        assert_width(host)
        host.screenshot(path=str(OUT / (name + '.png')), full_page=True)
    host.get_by_role('button', name='房间设置', exact=True).click()
    host.get_by_role('dialog').locator('.stack-settings > div').filter(has_text=f'浏览器{stamp}1').get_by_role('button', name='移出', exact=True).click()
    guest.get_by_role('heading', name='今晚，在哪一桌？').wait_for()
    assert guest.locator('audio').count() == 0, 'Microphone session retained after expulsion'
    assert not errors, errors
    print(json.dumps({'result': 'PASS', 'checks': ['real UI guest login', 'create and join room',
          'nine-seat desktop and portrait mobile layout', 'private hands', 'chat bubble',
          'two-browser WebRTC audio tracks', 'all-in settlement', 'room settings',
          'remote emoji bubbles and targeted reaction', 'personal settings preserve emoji', 'host expulsion and voice cleanup'],
          'screenshots': str(OUT)}, ensure_ascii=False))
    for context in contexts:
        context.close()
    browser.close()
