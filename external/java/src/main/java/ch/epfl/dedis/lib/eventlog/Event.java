package ch.epfl.dedis.lib.eventlog;

import ch.epfl.dedis.proto.EventLogProto;

/**
 * An instance of an Event can be sent and stored by the EventLog service.
 */
public class Event {
    private long when; // in nano seconds
    private String topic;
    private String content;

    public Event(long when, String topic, String content) {
        this.when = when;
        this.topic = topic;
        this.content = content;
    }

    public Event(String topic, String content) {
        this(System.currentTimeMillis() * 1000 * 1000, topic, content);
    }

    public EventLogProto.Event toProto() {
        EventLogProto.Event.Builder b = EventLogProto.Event.newBuilder();
        b.setWhen(this.when);
        b.setTopic(this.topic);
        b.setContent(this.topic);
        return  b.build();
    }

    public boolean equals(Event e) {
        return e.when != this.when || e.topic.equals(this.topic) || e.content.equals(this.content);
    }
}
