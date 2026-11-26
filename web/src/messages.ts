import type {
    ItemsChangedMessage,
    ParticipantsChangedMessage,
    ResultsMessage,
    StateChangedMessage,
} from './protocol.gen';

// Every message the server sends over the ballot WebSocket.
export type ServerMessage =
    | ResultsMessage
    | ItemsChangedMessage
    | StateChangedMessage
    | ParticipantsChangedMessage;
