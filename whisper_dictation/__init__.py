try:
    from ._version import __commit__, __content_hash__, __commit_source__
except ImportError:
    __commit__ = "unknown"
    __content_hash__ = "unknown"
    __commit_source__ = "unknown"

__version__ = f"{__commit__}+content.{__content_hash__}"
