FROM node:22-alpine

WORKDIR /app

# package-lock.json обязателен: npm ci даёт воспроизводимую установку.
COPY package.json package-lock.json ./
RUN npm ci

COPY . .

EXPOSE 5173

CMD ["npm", "run", "dev"]
