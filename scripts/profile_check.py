#!/usr/bin/env python3
"""Self-managed account and personal settings browser acceptance."""
import os
import secrets
import struct
import time
import zlib
from urllib.parse import urljoin

from playwright.sync_api import sync_playwright

BASE = os.environ.get('RIVER_TEST_URL', 'http://localhost:8080')


def avatar_png():
    def chunk(kind, value):
        return struct.pack('>I', len(value)) + kind + value + struct.pack('>I', zlib.crc32(kind + value))
    return (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', 2, 2, 8, 6, 0, 0, 0))
            + chunk(b'IDAT', zlib.compress((b'\x00' + b'\xe4\xbd\x80\xff' * 2) * 2)) + chunk(b'IEND', b''))


with sync_playwright() as p:
    browser = p.chromium.launch()
    page = browser.new_page(viewport={'width': 1280, 'height': 900})
    page_errors = []
    page.on('pageerror', lambda error: page_errors.append(str(error)))
    stamp = str(time.time_ns())
    email = f'ui-{stamp}@example.com'
    password = secrets.token_urlsafe(18)
    page.goto(BASE)
    page.locator('.segmented').get_by_role('button', name='注册', exact=True).click()
    page.get_by_label('你的昵称').fill('账号验收')
    page.get_by_label('邮箱', exact=True).fill(email)
    page.get_by_label('账号密码').fill(password)
    if os.environ.get('JOIN_PASSWORD'):
        page.get_by_label('全站加入密码').fill(os.environ['JOIN_PASSWORD'])
    page.get_by_role('button', name='创建账号', exact=True).click()
    page.get_by_role('heading', name='今晚，在哪一桌？').wait_for()
    page.get_by_role('button', name='退出登录', exact=True).click()
    page.locator('.segmented').get_by_role('button', name='登录', exact=True).click()
    page.get_by_label('邮箱', exact=True).fill(email)
    page.get_by_label('账号密码').fill(password)
    if os.environ.get('JOIN_PASSWORD'):
        page.get_by_label('全站加入密码').fill(os.environ['JOIN_PASSWORD'])
    page.locator('.auth-panel form').get_by_role('button', name='登录', exact=True).click()
    page.get_by_role('heading', name='今晚，在哪一桌？').wait_for()
    page.locator('.profile-trigger').click()
    dialog = page.get_by_role('dialog')
    dialog.get_by_label('昵称', exact=True).fill('保存后的昵称')
    dialog.get_by_label('音效音量').fill('0.3')
    dialog.get_by_role('button', name='保存设置', exact=True).click()
    page.locator('.profile-trigger').get_by_text('保存后的昵称', exact=True).wait_for()
    page.reload()
    page.locator('.profile-trigger').click()
    dialog = page.get_by_role('dialog')
    assert dialog.get_by_label('音效音量').input_value() == '0.3'
    dialog.locator('input[type=file]').set_input_files({'name': 'avatar.png', 'mimeType': 'image/png', 'buffer': avatar_png()})
    page.wait_for_function("document.querySelector('.profile-avatar img')?.getAttribute('src')?.startsWith('/api/avatars/')")
    response = page.request.get(urljoin(BASE, page.locator('.profile-avatar img').get_attribute('src')))
    assert response.status == 200 and response.headers['content-type'] == 'image/png'
    assert not page_errors, page_errors
    print('PASS: registration, logout, login, saved nickname/volume, profile reload, normalized avatar upload')
    browser.close()
