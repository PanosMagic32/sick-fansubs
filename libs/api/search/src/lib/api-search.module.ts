import { ApiBlogPostModule } from '@api/blog-post';
import { ApiProjectModule } from '@api/project';
import { Module } from '@nestjs/common';
import { ApiSearchController } from './api-search.controller';
import { ApiSearchService } from './api-search.service';

@Module({
  imports: [ApiBlogPostModule, ApiProjectModule],
  controllers: [ApiSearchController],
  providers: [ApiSearchService],
})
export class ApiSearchModule {}
