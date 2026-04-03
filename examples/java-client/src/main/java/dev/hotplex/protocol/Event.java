package dev.hotplex.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

/**
 * Event wrapper for AEP v1 Envelope.
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class Event {

    private String type;
    private Object data;

    public Event() {}

    public Event(String type, Object data) {
        this.type = type;
        this.data = data;
    }

    public String getType() {
        return type;
    }

    public void setType(String type) {
        this.type = type;
    }

    public Object getData() {
        return data;
    }

    public void setData(Object data) {
        this.data = data;
    }
}