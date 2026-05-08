from hotplex_client.types import (
    InitData, 
    WorkerType, 
    Envelope, 
    Event, 
    DoneStats, 
    DoneData
)

def test_init_data_structure():
    data = InitData(version="v1", worker_type=WorkerType.CLAUDE_CODE)
    assert data.version == "v1"
    assert data.worker_type == "claude_code"
    assert data.session_id is None

def test_envelope_to_dict():
    env = Envelope(
        version="v1",
        id="evt_1",
        seq=1,
        session_id="sess_1",
        timestamp=1000,
        event=Event(type="test", data={"foo": "bar"})
    )
    d = env.to_dict()
    assert d["version"] == "v1"
    assert d["id"] == "evt_1"
    assert d["event"]["type"] == "test"
    assert d["event"]["data"]["foo"] == "bar"

def test_done_data_stats():
    stats = DoneStats(duration_ms=100, total_tokens=50)
    data = DoneData(success=True, stats=stats)
    assert data.success is True
    assert data.stats.duration_ms == 100
    assert data.stats.total_tokens == 50
