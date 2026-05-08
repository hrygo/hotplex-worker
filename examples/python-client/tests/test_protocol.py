import json
import pytest
from hotplex_client.protocol import (
    encode_envelope,
    decode_envelope,
    create_input_envelope,
    generate_event_id,
)
from hotplex_client.types import Envelope, InputData, Priority

def test_generate_event_id():
    eid = generate_event_id()
    assert eid.startswith("evt_")
    assert len(eid) > 4

def test_encode_envelope():
    env = create_input_envelope("sess_123", "hello")
    encoded = encode_envelope(env)
    assert encoded.endswith("\n")
    
    data = json.loads(encoded)
    assert data["session_id"] == "sess_123"
    assert data["event"]["type"] == "input"
    assert data["event"]["data"]["content"] == "hello"

def test_decode_envelope():
    raw = '{"version":"aep/v1","id":"evt_1","seq":0,"session_id":"sess_1","timestamp":123,"event":{"type":"message.delta","data":{"message_id":"msg_1","content":"hi"}}}'
    env = decode_envelope(raw)
    
    assert isinstance(env, Envelope)
    assert env.event.type == "message.delta"
    assert env.event.data["content"] == "hi"
    assert env.session_id == "sess_1"

def test_decode_invalid_json():
    with pytest.raises(Exception): # InvalidMessageError
        decode_envelope("invalid json")

def test_priority_serialization():
    env = create_input_envelope("sess_1", "test")
    env.priority = Priority.DATA
    encoded = encode_envelope(env)
    data = json.loads(encoded)
    assert data["priority"] == "data"
