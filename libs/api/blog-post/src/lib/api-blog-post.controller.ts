import { Body, Controller, Delete, Get, Param, Post } from '@nestjs/common';

import { ApiBlogPostService } from './api-blog-post.service';
import { CreateBlogPostDto } from './dtos/create-blog-post.dto';
import { BlogPost } from './schemas/blog-post.schema';

@Controller('blog-post')
export class ApiBlogPostController {
  constructor(private apiBlogPostService: ApiBlogPostService) {}

  @Post()
  async create(@Body() createBlogPostDto: CreateBlogPostDto) {
    await this.apiBlogPostService.create(createBlogPostDto);
  }

  @Get()
  async findAll(): Promise<BlogPost[]> {
    return this.apiBlogPostService.findAll();
  }

  @Get(':id')
  async findOne(@Param('id') id: string): Promise<BlogPost | undefined> {
    return this.apiBlogPostService.findOne(id);
  }

  @Delete(':id')
  async delete(@Param('id') id: string) {
    return this.apiBlogPostService.delete(id);
  }
}
