"""Persist Finder layout on the mounted writable image before compression."""
import os
import sys
import base64
import subprocess
from ds_store import DSStore
from mac_alias import Alias

# statfs reports /private/var, while TMPDIR commonly uses /var. Resolve this
# symlink before mac_alias calculates a volume-relative background path.
root = os.path.realpath(sys.argv[1])
if '--verify' in sys.argv[2:]:
    with DSStore.open(os.path.join(root, '.DS_Store'), 'r') as store:
        saved = Alias.from_bytes(store['.']['icvp']['backgroundImageAlias'])
        assert saved.target.posix_path == '/.background/background.png'
        assert os.path.isfile(root + saved.target.posix_path)
        assert store['Tier0 Edge.app']['Iloc'] == (185, 205)
        assert store['Applications']['Iloc'] == (575, 205)
        assert os.path.isfile(os.path.join(root, 'Applications'))
        subprocess.run(['swift', os.path.join(os.path.dirname(__file__), 'macos-dmg-verify.swift'),
                        base64.b64encode(store['.']['icvp']['backgroundImageAlias']).decode(),
                        root + saved.target.posix_path], check=True)
    print('Final DMG background and drag targets verified after remount')
    sys.exit(0)
background_alias = Alias.for_file(os.path.join(root, '.background', 'background.png'))
assert background_alias.target.posix_path == b'/.background/background.png'
background = background_alias.to_bytes()
with DSStore.open(os.path.join(root, '.DS_Store'), 'w+') as store:
    store['.']['vstl'] = ('type', b'icnv')
    store['.']['bwsp'] = {
        'WindowBounds': '{{180, 100}, {760, 528}}',
        'ShowToolbar': False, 'ShowStatusBar': False, 'ShowPathbar': False,
        'ShowSidebar': False, 'ShowTabView': False, 'ContainerShowSidebar': False,
        'SidebarWidth': 0,
    }
    store['.']['icvp'] = {
        'viewOptionsVersion': 1, 'backgroundType': 2,
        'backgroundImageAlias': background, 'backgroundColorRed': 1.0,
        'backgroundColorGreen': 1.0, 'backgroundColorBlue': 1.0,
        'iconSize': 96.0, 'textSize': 14.0, 'labelOnBottom': True,
        'showItemInfo': False, 'showIconPreview': True,
        'arrangeBy': 'none', 'gridOffsetX': 0.0, 'gridOffsetY': 0.0, 'gridSpacing': 100.0,
    }
    store['Tier0 Edge.app']['Iloc'] = (185, 205)
    store['Applications']['Iloc'] = (575, 205)
    store['安装说明.txt']['Iloc'] = (650, 380)

with DSStore.open(os.path.join(root, '.DS_Store'), 'r') as store:
    assert store['Tier0 Edge.app']['Iloc'] == (185, 205)
    assert store['Applications']['Iloc'] == (575, 205)
    assert store['.']['icvp']['backgroundType'] == 2
print('Finder layout verified: app left, Applications right, instructions below')
