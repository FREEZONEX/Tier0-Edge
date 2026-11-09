import { createContext } from 'react';

// Keep background editors mounted while a global dialog owns interaction.
export const ForegroundDialogContext = createContext(false);
