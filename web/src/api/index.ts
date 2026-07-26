import { MockAPI } from './mock'
import { RealAPI } from './client'

const USE_MOCK = import.meta.env.VITE_USE_MOCK === 'true'
export const API = USE_MOCK ? MockAPI : RealAPI
