<script setup lang="ts">
import { NConfigProvider, NGlobalStyle, darkTheme } from 'naive-ui'
import { RouterView } from 'vue-router'
import { naiveThemeOverrides } from '@/styles/tokens'
</script>

<template>
  <NConfigProvider :theme="darkTheme" :theme-overrides="naiveThemeOverrides">
    <NGlobalStyle />
    <RouterView />
  </NConfigProvider>
</template>
