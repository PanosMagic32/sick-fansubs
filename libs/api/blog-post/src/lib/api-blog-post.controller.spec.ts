import { Test } from '@nestjs/testing';

import { ApiBlogPostController } from './api-blog-post.controller';
import { ApiBlogPostService } from './api-blog-post.service';

describe('ApiBlogPostController', () => {
  let controller: ApiBlogPostController;

  beforeEach(async () => {
    const module = await Test.createTestingModule({
      providers: [ApiBlogPostService],
      controllers: [ApiBlogPostController],
    }).compile();

    controller = module.get(ApiBlogPostController);
  });

  it('should be defined', () => {
    expect(controller).toBeTruthy();
  });
});
