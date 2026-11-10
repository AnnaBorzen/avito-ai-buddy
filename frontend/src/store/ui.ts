import { create } from 'zustand'

type UiState = {
  autoRefresh: boolean
  toggleAutoRefresh: () => void
}

export const useUiStore = create<UiState>((set) => ({
  autoRefresh: true,
  toggleAutoRefresh: () => set((state) => ({ autoRefresh: !state.autoRefresh })),
}))
