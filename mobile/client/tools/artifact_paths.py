"""Only the approved evidence compatibility mount may leave the source workspace."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def _no_links(path):
    for current in (path, *path.parents):
        if current.is_symlink() or current.is_junction():
            raise ValueError('Linked artifact storage or source ancestor forbidden')

def checked(path, root=ROOT):
    root = Path(root).absolute()
    path = Path(path).absolute()
    if '..' in path.parts:
        raise ValueError('Non-canonical workspace path')
    alias = root / 'docs/evidence'
    storage = root.parent / (root.name + '-Artifacts') / 'evidence'
    _no_links(root)
    if path.is_relative_to(root):
        for current in (path, *path.parents):
            if not current.is_relative_to(root):
                break
            if current.is_symlink() or current.is_junction():
                if current != alias or current.resolve() != storage:
                    raise ValueError('Linked workspace path forbidden')
                _no_links(storage)
        expected = storage / path.relative_to(alias) if path.is_relative_to(alias) and (alias.is_symlink() or alias.is_junction()) else path
        if path.resolve() != expected:
            raise ValueError('Resolved workspace path escaped approved location')
    elif path.is_relative_to(storage):
        _no_links(path)
        if path.resolve() != path:
            raise ValueError('Resolved artifact path escaped approved location')
    else:
        raise ValueError('Path must remain in workspace or approved evidence storage')
    return path

def verify_mount(root=ROOT):
    root = Path(root).absolute()
    alias = root / 'docs/evidence'
    storage = root.parent / (root.name + '-Artifacts') / 'evidence'
    if not (alias.is_symlink() or alias.is_junction()) or alias.resolve() != storage or not storage.is_dir():
        raise ValueError('Evidence compatibility mount missing or changed')
    checked(alias, root)
    return {'compatibilityPath': str(alias), 'physicalPath': str(storage)}
