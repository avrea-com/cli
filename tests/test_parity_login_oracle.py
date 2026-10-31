"""The login fixture must not intercept ordinary DNS or callback HTTP."""

from http.server import HTTPServer
from scripts.parity_login_oracle import loopback_fqdn
from unittest.mock import Mock
from unittest.mock import patch
import http.server
import pytest


@pytest.mark.parametrize("name", ["localhost", "127.0.0.1", "::1"])
def test_loopback_name_does_not_use_reverse_dns(name):
    resolve = Mock(side_effect=AssertionError("external reverse DNS"))
    assert loopback_fqdn(name, resolve) == "localhost"
    resolve.assert_not_called()


@pytest.mark.parametrize("name", ["", "example.test", "192.0.2.1"])
def test_other_names_keep_normal_resolution(name):
    resolve = Mock(return_value="resolved.example.test")
    assert loopback_fqdn(name, resolve) == "resolved.example.test"
    resolve.assert_called_once_with(name)


def test_real_callback_listener_can_bind_without_reverse_dns():
    resolve = Mock(side_effect=AssertionError("external reverse DNS"))
    with (
        patch("socket.getfqdn", side_effect=lambda name="": loopback_fqdn(name, resolve)),
        HTTPServer(("127.0.0.1", 0), http.server.BaseHTTPRequestHandler) as server,
    ):
        assert server.server_name == "localhost"
        assert server.server_port > 0
    resolve.assert_not_called()
