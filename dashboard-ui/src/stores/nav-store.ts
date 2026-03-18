import { create } from 'zustand'

export interface NavState {
  isNavigating: boolean
  setNavigating: (v: boolean) => void
}

export const useNavStore = create<NavState>((set) => ({
  isNavigating: false,
  setNavigating: (v) => set({ isNavigating: v }),
}))
